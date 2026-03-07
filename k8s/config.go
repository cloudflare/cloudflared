// Package k8s provides Kubernetes service discovery and automatic ingress rule
// generation for Cloudflare Tunnel. When running inside (or with access to) a
// Kubernetes cluster, this package can watch for annotated Services and
// automatically expose them through the tunnel without manual ingress
// configuration.
package k8s

import (
	"fmt"
	"time"
)

const (
	// AnnotationEnabled is the annotation key that must be set to "true" on a
	// Kubernetes Service for it to be discovered and exposed through the tunnel.
	AnnotationEnabled = "cloudflared.cloudflare.com/tunnel"

	// AnnotationHostname optionally overrides the hostname that will be used
	// in the generated ingress rule. If not set, a hostname is synthesised from
	// the service name, namespace, and the configured base domain.
	AnnotationHostname = "cloudflared.cloudflare.com/hostname"

	// AnnotationPath optionally specifies a path regex for the ingress rule.
	AnnotationPath = "cloudflared.cloudflare.com/path"

	// AnnotationScheme overrides the scheme used to reach the origin.
	// Defaults to "http" for non-TLS ports and "https" for port 443.
	AnnotationScheme = "cloudflared.cloudflare.com/scheme"

	// AnnotationPort overrides which service port to route traffic to when
	// the service exposes multiple ports. If unset the first port is used.
	AnnotationPort = "cloudflared.cloudflare.com/port"

	// AnnotationNoTLSVerify disables TLS verification for the origin.
	AnnotationNoTLSVerify = "cloudflared.cloudflare.com/no-tls-verify"

	// AnnotationOriginServerName sets the SNI for TLS connections to the origin.
	AnnotationOriginServerName = "cloudflared.cloudflare.com/origin-server-name"

	// DefaultResyncPeriod is how often the informer re-lists all Services even
	// if no watch events have been received.
	DefaultResyncPeriod = 30 * time.Second
)

// Config holds the user-facing configuration for the Kubernetes integration.
type Config struct {
	// Enabled turns the Kubernetes watcher on.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Namespace limits discovery to a single namespace. Empty means all namespaces.
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// BaseDomain is the base domain appended when generating hostnames, e.g.
	// "example.com" results in "<svc>-<ns>.example.com".
	BaseDomain string `yaml:"baseDomain,omitempty" json:"baseDomain,omitempty"`

	// KubeconfigPath is an optional path to a kubeconfig file. When empty the
	// in-cluster config is used.
	KubeconfigPath string `yaml:"kubeconfigPath,omitempty" json:"kubeconfigPath,omitempty"`

	// ExposeAPIServer, when true, creates an ingress rule for the Kubernetes
	// API server (typically at https://kubernetes.default.svc).
	ExposeAPIServer bool `yaml:"exposeAPIServer,omitempty" json:"exposeAPIServer,omitempty"`

	// APIServerHostname is the public hostname through which the K8s API server
	// will be reachable. Required when ExposeAPIServer is true.
	APIServerHostname string `yaml:"apiServerHostname,omitempty" json:"apiServerHostname,omitempty"`

	// LabelSelector is an optional Kubernetes label selector (e.g. "app=web")
	// to filter which services to consider. Works in addition to the annotation
	// check.
	LabelSelector string `yaml:"labelSelector,omitempty" json:"labelSelector,omitempty"`

	// ResyncPeriod controls how often the full service list is re-synchronised.
	// Defaults to DefaultResyncPeriod.
	ResyncPeriod time.Duration `yaml:"resyncPeriod,omitempty" json:"resyncPeriod,omitempty"`
}

// Validate checks that the configuration is internally consistent.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.BaseDomain == "" {
		return fmt.Errorf("kubernetes.baseDomain is required when kubernetes integration is enabled")
	}
	if c.ExposeAPIServer && c.APIServerHostname == "" {
		return fmt.Errorf("kubernetes.apiServerHostname is required when exposeAPIServer is true")
	}
	if c.ResyncPeriod == 0 {
		c.ResyncPeriod = DefaultResyncPeriod
	}
	return nil
}
