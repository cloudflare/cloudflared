package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/k8s"
	"github.com/cloudflare/cloudflared/logger"
)

const (
	k8sBaseDomainFlag        = "k8s-base-domain"
	k8sNamespaceFlag         = "k8s-namespace"
	k8sKubeconfigFlag        = "k8s-kubeconfig"
	k8sExposeAPIServerFlag   = "k8s-expose-api-server"
	k8sAPIServerHostnameFlag = "k8s-api-server-hostname"
	k8sLabelSelectorFlag     = "k8s-label-selector"
	k8sOutputFormatFlag      = "k8s-output"
)

func buildKubernetesSubcommand() *cli.Command {
	return &cli.Command{
		Name:     "kubernetes",
		Aliases:  []string{"k8s"},
		Category: "Tunnel",
		Usage:    "Discover and manage Kubernetes services exposed through Cloudflare Tunnel",
		Description: `  The kubernetes subcommand provides native integration between cloudflared and
  Kubernetes clusters. It can automatically discover annotated Kubernetes
  services and generate ingress rules for them.

  To mark a service for exposure through the tunnel, add the annotation:
    cloudflared.cloudflare.com/tunnel: "true"

  Optional annotations:
    cloudflared.cloudflare.com/hostname:           Override the public hostname
    cloudflared.cloudflare.com/path:               Path regex for the ingress rule
    cloudflared.cloudflare.com/scheme:              Origin scheme (http/https)
    cloudflared.cloudflare.com/port:                Select which port to proxy
    cloudflared.cloudflare.com/no-tls-verify:       Disable TLS verification
    cloudflared.cloudflare.com/origin-server-name:  Set SNI for TLS

  Example:
    # Discover services from the current cluster
    cloudflared tunnel kubernetes discover --k8s-base-domain example.com

    # Watch for changes continuously
    cloudflared tunnel kubernetes watch --k8s-base-domain example.com

    # Generate an ingress config YAML snippet
    cloudflared tunnel kubernetes generate-config --k8s-base-domain example.com`,
		Subcommands: []*cli.Command{
			buildK8sDiscoverCommand(),
			buildK8sWatchCommand(),
			buildK8sGenerateConfigCommand(),
		},
	}
}

func k8sFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    k8sBaseDomainFlag,
			Usage:   "Base domain for auto-generated hostnames (e.g. example.com). Services will be exposed as <name>-<namespace>.example.com",
			EnvVars: []string{"TUNNEL_K8S_BASE_DOMAIN"},
		},
		&cli.StringFlag{
			Name:    k8sNamespaceFlag,
			Usage:   "Limit discovery to a specific Kubernetes namespace. Empty means all namespaces.",
			EnvVars: []string{"TUNNEL_K8S_NAMESPACE"},
		},
		&cli.StringFlag{
			Name:    k8sKubeconfigFlag,
			Usage:   "Path to a kubeconfig file. When empty, in-cluster config is used.",
			EnvVars: []string{"KUBECONFIG"},
		},
		&cli.BoolFlag{
			Name:    k8sExposeAPIServerFlag,
			Usage:   "Also expose the Kubernetes API server through the tunnel",
			EnvVars: []string{"TUNNEL_K8S_EXPOSE_API_SERVER"},
		},
		&cli.StringFlag{
			Name:    k8sAPIServerHostnameFlag,
			Usage:   "Public hostname for the Kubernetes API server (required when --k8s-expose-api-server is set)",
			EnvVars: []string{"TUNNEL_K8S_API_SERVER_HOSTNAME"},
		},
		&cli.StringFlag{
			Name:    k8sLabelSelectorFlag,
			Usage:   "Kubernetes label selector to filter services (e.g. app=web)",
			EnvVars: []string{"TUNNEL_K8S_LABEL_SELECTOR"},
		},
		&cli.StringFlag{
			Name:  k8sOutputFormatFlag,
			Usage: "Output format: json, yaml, or table (default: table)",
			Value: "table",
		},
	}
}

func k8sConfigFromCLI(c *cli.Context) *k8s.Config {
	return &k8s.Config{
		Enabled:           true,
		BaseDomain:        c.String(k8sBaseDomainFlag),
		Namespace:         c.String(k8sNamespaceFlag),
		KubeconfigPath:    c.String(k8sKubeconfigFlag),
		ExposeAPIServer:   c.Bool(k8sExposeAPIServerFlag),
		APIServerHostname: c.String(k8sAPIServerHostnameFlag),
		LabelSelector:     c.String(k8sLabelSelectorFlag),
	}
}

// -----------------------------------------------------------------------
// discover subcommand
// -----------------------------------------------------------------------

func buildK8sDiscoverCommand() *cli.Command {
	return &cli.Command{
		Name:   "discover",
		Usage:  "Discover annotated Kubernetes services",
		Flags:  k8sFlags(),
		Action: cliutil.ConfiguredAction(k8sDiscoverAction),
	}
}

func k8sDiscoverAction(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	cfg := k8sConfigFromCLI(c)
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	services, err := k8s.DiscoverServices(ctx, cfg, log)
	if err != nil {
		return err
	}

	return printServices(services, c.String(k8sOutputFormatFlag), log)
}

// -----------------------------------------------------------------------
// watch subcommand
// -----------------------------------------------------------------------

func buildK8sWatchCommand() *cli.Command {
	return &cli.Command{
		Name:   "watch",
		Usage:  "Continuously watch for Kubernetes service changes",
		Flags:  k8sFlags(),
		Action: cliutil.ConfiguredAction(k8sWatchAction),
	}
}

func k8sWatchAction(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	cfg := k8sConfigFromCLI(c)
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	outputFormat := c.String(k8sOutputFormatFlag)

	watcher := k8s.NewWatcher(cfg, log, func(services []k8s.ServiceInfo) {
		log.Info().Int("count", len(services)).Msg("Service change detected")
		if err := printServices(services, outputFormat, log); err != nil {
			log.Err(err).Msg("Failed to print services")
		}
	})

	// Handle OS signals for graceful shutdown
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigC
		log.Info().Msg("Received shutdown signal, stopping watcher...")
		cancel()
	}()

	watcher.Run(ctx)
	return nil
}

// -----------------------------------------------------------------------
// generate-config subcommand
// -----------------------------------------------------------------------

func buildK8sGenerateConfigCommand() *cli.Command {
	return &cli.Command{
		Name:   "generate-config",
		Usage:  "Generate cloudflared ingress configuration from discovered Kubernetes services",
		Flags:  k8sFlags(),
		Action: cliutil.ConfiguredAction(k8sGenerateConfigAction),
	}
}

func k8sGenerateConfigAction(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	cfg := k8sConfigFromCLI(c)
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	services, err := k8s.DiscoverServices(ctx, cfg, log)
	if err != nil {
		return err
	}

	if len(services) == 0 {
		log.Warn().Msg("No annotated Kubernetes services found")
		return nil
	}

	rules := k8s.GenerateIngressRules(services, log)

	// Output as YAML config snippet
	fmt.Println("# Auto-generated cloudflared ingress configuration from Kubernetes services")
	fmt.Println("# Add the following to your cloudflared config.yml under the 'ingress' key:")
	fmt.Println("ingress:")
	for _, r := range rules {
		if r.Hostname != "" {
			fmt.Printf("  - hostname: %s\n", r.Hostname)
		} else {
			fmt.Println("  - hostname: \"*\"")
		}
		if r.Path != "" {
			fmt.Printf("    path: %s\n", r.Path)
		}
		fmt.Printf("    service: %s\n", r.Service)

		hasNoTLS := r.OriginRequest.NoTLSVerify != nil && *r.OriginRequest.NoTLSVerify
		hasSNI := r.OriginRequest.OriginServerName != nil && *r.OriginRequest.OriginServerName != ""
		if hasNoTLS || hasSNI {
			fmt.Println("    originRequest:")
			if hasNoTLS {
				fmt.Println("      noTLSVerify: true")
			}
			if hasSNI {
				fmt.Printf("      originServerName: %s\n", *r.OriginRequest.OriginServerName)
			}
		}
	}
	// Add catch-all
	fmt.Println("  - service: http_status:404")

	return nil
}

// -----------------------------------------------------------------------
// Output helpers
// -----------------------------------------------------------------------

func printServices(services []k8s.ServiceInfo, format string, log *zerolog.Logger) error {
	if len(services) == 0 {
		log.Info().Msg("No annotated Kubernetes services found")
		return nil
	}

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(services)
	case "yaml":
		for _, s := range services {
			fmt.Printf("- name: %s\n  namespace: %s\n  hostname: %s\n  origin: %s\n",
				s.Name, s.Namespace, s.Hostname, s.OriginURL())
			if s.Path != "" {
				fmt.Printf("  path: %s\n", s.Path)
			}
		}
		return nil
	default: // table
		fmt.Printf("%-30s %-15s %-40s %-35s %s\n", "SERVICE", "NAMESPACE", "HOSTNAME", "ORIGIN", "PATH")
		fmt.Printf("%-30s %-15s %-40s %-35s %s\n", "-------", "---------", "--------", "------", "----")
		for _, s := range services {
			fmt.Printf("%-30s %-15s %-40s %-35s %s\n", s.Name, s.Namespace, s.Hostname, s.OriginURL(), s.Path)
		}
		return nil
	}
}
