package k8s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ServiceInfo represents a discovered Kubernetes Service with enough
// information to build an ingress rule.
type ServiceInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	// ClusterIP is the internal IP of the service.
	ClusterIP string `json:"clusterIP"`
	// Port is the port selected for proxying.
	Port int32 `json:"port"`
	// PortName is the name of the selected port (if any).
	PortName string `json:"portName,omitempty"`
	// Scheme is http or https.
	Scheme string `json:"scheme"`
	// Hostname is the fully-qualified public hostname.
	Hostname string `json:"hostname"`
	// Path is an optional path regex from the annotation.
	Path string `json:"path,omitempty"`
	// NoTLSVerify disables TLS certificate verification for the origin.
	NoTLSVerify bool `json:"noTLSVerify,omitempty"`
	// OriginServerName is the SNI server name for TLS.
	OriginServerName string `json:"originServerName,omitempty"`
}

// OriginURL returns the URL that cloudflared should proxy traffic to.
func (s *ServiceInfo) OriginURL() string {
	return fmt.Sprintf("%s://%s:%d", s.Scheme, s.ClusterIP, s.Port)
}

// -----------------------------------------------------------------------
// Lightweight Kubernetes client — no dependency on client-go
// -----------------------------------------------------------------------

// kubeClient is a minimal Kubernetes REST client that can list and watch
// Service resources.
type kubeClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
	log        *zerolog.Logger
}

// newInClusterClient builds a kubeClient from the standard in-cluster service
// account files.
func newInClusterClient(log *zerolog.Logger) (*kubeClient, error) {
	const (
		tokenPath  = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // Not a credential, this is a well-known file path
		caPath     = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		nsPath     = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
		serviceEnv = "KUBERNETES_SERVICE_HOST"
		portEnv    = "KUBERNETES_SERVICE_PORT"
	)

	host := os.Getenv(serviceEnv)
	port := os.Getenv(portEnv)
	if host == "" || port == "" {
		return nil, fmt.Errorf("not running inside a Kubernetes cluster (KUBERNETES_SERVICE_HOST/PORT not set)")
	}

	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read service account token: %w", err)
	}

	// Load the cluster CA certificate for TLS verification against the API server.
	httpClient := &http.Client{Timeout: 30 * time.Second}
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		// If CA cert is not available, fall back to default system trust store
		// but log a warning — TLS may fail for self-signed API server certs.
		if log != nil {
			log.Warn().Err(err).Msg("Could not load in-cluster CA cert, falling back to system trust store")
		}
	} else {
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caCertPool,
				MinVersion: tls.VersionTLS12,
			},
		}
	}

	return &kubeClient{
		baseURL:    fmt.Sprintf("https://%s:%s", host, port),
		httpClient: httpClient,
		token:      strings.TrimSpace(string(tokenBytes)),
		log:        log,
	}, nil
}

// newKubeconfigClient builds a kubeClient from a kubeconfig-style file.
// This is a simplified parser that reads the first cluster/user.
func newKubeconfigClient(kubeconfigPath string, log *zerolog.Logger) (*kubeClient, error) {
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read kubeconfig %s: %w", kubeconfigPath, err)
	}

	kc, err := parseKubeconfig(data)
	if err != nil {
		return nil, err
	}

	return &kubeClient{
		baseURL:    kc.server,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      kc.token,
		log:        log,
	}, nil
}

// kubeconfigInfo holds the minimal info parsed from a kubeconfig.
type kubeconfigInfo struct {
	server string
	token  string
}

// parseKubeconfig is a very simple YAML→JSON-style parser for kubeconfig.
// It reads the current-context and extracts the server URL and bearer token.
// For a production implementation you would use "k8s.io/client-go/tools/clientcmd".
func parseKubeconfig(data []byte) (*kubeconfigInfo, error) {
	// Attempt a simple JSON parse (kubeconfig can be JSON or YAML).
	// For YAML we do a basic line-scan fallback.
	type namedCluster struct {
		Name    string `json:"name"`
		Cluster struct {
			Server string `json:"server"`
		} `json:"cluster"`
	}
	type namedUser struct {
		Name string `json:"name"`
		User struct {
			Token string `json:"token"`
		} `json:"user"`
	}
	type namedContext struct {
		Name    string `json:"name"`
		Context struct {
			Cluster string `json:"cluster"`
			User    string `json:"user"`
		} `json:"context"`
	}
	type kubeConfig struct {
		CurrentContext string         `json:"current-context"`
		Clusters       []namedCluster `json:"clusters"`
		Users          []namedUser    `json:"users"`
		Contexts       []namedContext `json:"contexts"`
	}

	var kc kubeConfig
	if err := json.Unmarshal(data, &kc); err != nil {
		// Not valid JSON – return a generic error for now.
		return nil, fmt.Errorf("failed to parse kubeconfig: %w (only JSON format is supported in this implementation)", err)
	}

	// Resolve current context.
	var clusterName, userName string
	for _, ctx := range kc.Contexts {
		if ctx.Name == kc.CurrentContext {
			clusterName = ctx.Context.Cluster
			userName = ctx.Context.User
			break
		}
	}
	if clusterName == "" {
		return nil, fmt.Errorf("current-context %q not found in kubeconfig", kc.CurrentContext)
	}

	var server, token string
	for _, c := range kc.Clusters {
		if c.Name == clusterName {
			server = c.Cluster.Server
			break
		}
	}
	for _, u := range kc.Users {
		if u.Name == userName {
			token = u.User.Token
			break
		}
	}
	if server == "" {
		return nil, fmt.Errorf("cluster %q server URL not found in kubeconfig", clusterName)
	}

	return &kubeconfigInfo{server: server, token: token}, nil
}

// do executes an authenticated HTTP request against the API server.
func (kc *kubeClient) do(ctx context.Context, method, path string) ([]byte, error) {
	url := kc.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	if kc.token != "" {
		req.Header.Set("Authorization", "Bearer "+kc.token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := kc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("k8s API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading k8s API response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("k8s API returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// -----------------------------------------------------------------------
// K8s API response types (minimal)
// -----------------------------------------------------------------------

type serviceList struct {
	Items []serviceItem `json:"items"`
}

type serviceItem struct {
	Metadata objectMeta  `json:"metadata"`
	Spec     serviceSpec `json:"spec"`
}

type objectMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type serviceSpec struct {
	ClusterIP string        `json:"clusterIP"`
	Ports     []servicePort `json:"ports"`
	Type      string        `json:"type"`
}

type servicePort struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// -----------------------------------------------------------------------
// Discovery logic
// -----------------------------------------------------------------------

// DiscoverServices queries the Kubernetes API for Services annotated with
// AnnotationEnabled = "true" and returns ServiceInfo descriptors.
func DiscoverServices(ctx context.Context, cfg *Config, log *zerolog.Logger) ([]ServiceInfo, error) {
	client, err := buildClient(cfg, log)
	if err != nil {
		return nil, err
	}

	path := "/api/v1/services"
	if cfg.Namespace != "" {
		path = fmt.Sprintf("/api/v1/namespaces/%s/services", cfg.Namespace)
	}
	if cfg.LabelSelector != "" {
		path += "?labelSelector=" + url.QueryEscape(cfg.LabelSelector)
	}

	body, err := client.do(ctx, http.MethodGet, path)
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}

	var list serviceList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("parsing service list: %w", err)
	}

	services := make([]ServiceInfo, 0, len(list.Items))
	for _, item := range list.Items {
		ann := item.Metadata.Annotations
		if ann == nil {
			continue
		}
		enabled, ok := ann[AnnotationEnabled]
		if !ok || !isTrue(enabled) {
			continue
		}

		si, err := serviceInfoFromItem(item, cfg)
		if err != nil {
			log.Warn().Err(err).
				Str("service", item.Metadata.Name).
				Str("namespace", item.Metadata.Namespace).
				Msg("Skipping service due to error")
			continue
		}
		services = append(services, *si)
	}

	// Optionally expose the API server itself.
	if cfg.ExposeAPIServer && cfg.APIServerHostname != "" {
		apiSvc := ServiceInfo{
			Name:        "kubernetes-api",
			Namespace:   "default",
			ClusterIP:   strings.TrimPrefix(strings.TrimPrefix(client.baseURL, "https://"), "http://"),
			Port:        443,
			Scheme:      "https",
			Hostname:    cfg.APIServerHostname,
			NoTLSVerify: true, // API server cert may not match the public hostname
		}
		// If the baseURL contains host:port, split it.
		if hp := strings.SplitN(apiSvc.ClusterIP, ":", 2); len(hp) == 2 {
			apiSvc.ClusterIP = hp[0]
			if p, err := parseInt32(hp[1]); err == nil {
				apiSvc.Port = p
			}
		}
		services = append(services, apiSvc)
	}

	return services, nil
}

// serviceInfoFromItem converts a raw Kubernetes service item into a ServiceInfo.
func serviceInfoFromItem(item serviceItem, cfg *Config) (*ServiceInfo, error) {
	ann := item.Metadata.Annotations
	spec := item.Spec

	if spec.ClusterIP == "" || spec.ClusterIP == "None" {
		return nil, fmt.Errorf("service %s/%s has no ClusterIP (headless services are not supported)",
			item.Metadata.Namespace, item.Metadata.Name)
	}

	port, portName, err := selectPort(spec.Ports, ann[AnnotationPort])
	if err != nil {
		return nil, err
	}

	scheme := "http"
	if v, ok := ann[AnnotationScheme]; ok {
		scheme = v
	} else if port == 443 {
		scheme = "https"
	}

	hostname := ann[AnnotationHostname]
	if hostname == "" {
		hostname = fmt.Sprintf("%s-%s.%s", item.Metadata.Name, item.Metadata.Namespace, cfg.BaseDomain)
	}

	si := &ServiceInfo{
		Name:      item.Metadata.Name,
		Namespace: item.Metadata.Namespace,
		ClusterIP: spec.ClusterIP,
		Port:      port,
		PortName:  portName,
		Scheme:    scheme,
		Hostname:  hostname,
		Path:      ann[AnnotationPath],
	}

	if v, ok := ann[AnnotationNoTLSVerify]; ok && isTrue(v) {
		si.NoTLSVerify = true
	}
	if v, ok := ann[AnnotationOriginServerName]; ok {
		si.OriginServerName = v
	}

	return si, nil
}

// selectPort picks the port to use from the service's port list.
func selectPort(ports []servicePort, portAnnotation string) (int32, string, error) {
	if len(ports) == 0 {
		return 0, "", fmt.Errorf("service has no ports")
	}
	if portAnnotation == "" {
		return ports[0].Port, ports[0].Name, nil
	}
	// Match by name first, then by number.
	for _, p := range ports {
		if p.Name == portAnnotation {
			return p.Port, p.Name, nil
		}
	}
	portNum, err := parseInt32(portAnnotation)
	if err == nil {
		for _, p := range ports {
			if p.Port == portNum {
				return p.Port, p.Name, nil
			}
		}
	}
	return 0, "", fmt.Errorf("port %q not found in service", portAnnotation)
}

func buildClient(cfg *Config, log *zerolog.Logger) (*kubeClient, error) {
	if cfg.KubeconfigPath != "" {
		path := cfg.KubeconfigPath
		if strings.HasPrefix(path, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, path[1:])
			}
		}
		return newKubeconfigClient(path, log)
	}
	return newInClusterClient(log)
}

func isTrue(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

func parseInt32(s string) (int32, error) {
	var v int32
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
