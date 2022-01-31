package dnsserver

import (
	"flag"
	"fmt"
	"net"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/caddy/caddyfile"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/parse"
	"github.com/coredns/coredns/plugin/pkg/transport"

	"github.com/miekg/dns"
)

const serverType = "dns"

// Any flags defined here, need to be namespaced to the serverType other
// wise they potentially clash with other server types.
func init() {
	flag.StringVar(&Port, serverType+".port", DefaultPort, "Default port")
	flag.StringVar(&Port, "p", DefaultPort, "Default port")

	caddy.RegisterServerType(serverType, caddy.ServerType{
		Directives: func() []string { return Directives },
		DefaultInput: func() caddy.Input {
			return caddy.CaddyfileInput{
				Filepath:       "Corefile",
				Contents:       []byte(".:" + Port + " {\nwhoami\nlog\n}\n"),
				ServerTypeName: serverType,
			}
		},
		NewContext: newContext,
	})
}

func newContext(i *caddy.Instance) caddy.Context {
	return &dnsContext{keysToConfigs: make(map[string]*Config)}
}

type dnsContext struct {
	keysToConfigs map[string]*Config

	// configs is the master list of all site configs.
	configs []*Config
}

func (h *dnsContext) saveConfig(key string, cfg *Config) {
	h.configs = append(h.configs, cfg)
	h.keysToConfigs[key] = cfg
}

// Compile-time check to ensure dnsContext implements the caddy.Context interface
var _ caddy.Context = &dnsContext{}

// InspectServerBlocks make sure that everything checks out before
// executing directives and otherwise prepares the directives to
// be parsed and executed.
func (h *dnsContext) InspectServerBlocks(sourceFile string, serverBlocks []caddyfile.ServerBlock) ([]caddyfile.ServerBlock, error) {
	// Normalize and check all the zone names and check for duplicates
	for ib, s := range serverBlocks {
		// Walk the s.Keys and expand any reverse address in their proper DNS in-addr zones. If the expansions leads for
		// more than one reverse zone, replace the current value and add the rest to s.Keys.
		zoneAddrs := []zoneAddr{}
		for ik, k := range s.Keys {
			trans, k1 := parse.Transport(k) // get rid of any dns:// or other scheme.
			hosts, port, err := plugin.SplitHostPort(k1)
			// We need to make this a fully qualified domain name to catch all errors here and not later when
			// plugin.Normalize is called again on these strings, with the prime difference being that the domain
			// name is fully qualified. This was found by fuzzing where "ȶ" is deemed OK, but "ȶ." is not (might be a
			// bug in miekg/dns actually). But here we were checking ȶ, which is OK, and later we barf in ȶ. leading to
			// "index out of range".
			for ih := range hosts {
				_, _, err := plugin.SplitHostPort(dns.Fqdn(hosts[ih]))
				if err != nil {
					return nil, err
				}
			}
			if err != nil {
				return nil, err
			}

			if port == "" {
				switch trans {
				case transport.DNS:
					port = Port
				case transport.TLS:
					port = transport.TLSPort
				case transport.GRPC:
					port = transport.GRPCPort
				case transport.HTTPS:
					port = transport.HTTPSPort
				}
			}

			if len(hosts) > 1 {
				s.Keys[ik] = hosts[0] + ":" + port // replace for the first
				for _, h := range hosts[1:] {      // add the rest
					s.Keys = append(s.Keys, h+":"+port)
				}
			}
			for i := range hosts {
				zoneAddrs = append(zoneAddrs, zoneAddr{Zone: dns.Fqdn(hosts[i]), Port: port, Transport: trans})
			}
		}

		serverBlocks[ib].Keys = s.Keys // important to save back the new keys that are potentially created here.

		var firstConfigInBlock *Config

		for ik := range s.Keys {
			za := zoneAddrs[ik]
			s.Keys[ik] = za.String()
			// Save the config to our master list, and key it for lookups.
			cfg := &Config{
				Zone:        za.Zone,
				ListenHosts: []string{""},
				Port:        za.Port,
				Transport:   za.Transport,
			}

			// Set reference to the first config in the current block.
			// This is used later by MakeServers to share a single plugin list
			// for all zones in a server block.
			if ik == 0 {
				firstConfigInBlock = cfg
			}
			cfg.firstConfigInBlock = firstConfigInBlock

			keyConfig := keyForConfig(ib, ik)
			h.saveConfig(keyConfig, cfg)
		}
	}
	return serverBlocks, nil
}

// MakeServers uses the newly-created siteConfigs to create and return a list of server instances.
func (h *dnsContext) MakeServers() ([]caddy.Server, error) {

	// Now that all Keys and Directives are parsed and initialized
	// lets verify that there is no overlap on the zones and addresses to listen for
	errValid := h.validateZonesAndListeningAddresses()
	if errValid != nil {
		return nil, errValid
	}

	// Copy the Plugin, ListenHosts and Debug from first config in the block
	// to all other config in the same block . Doing this results in zones
	// sharing the same plugin instances and settings as other zones in
	// the same block.
	for _, c := range h.configs {
		c.Plugin = c.firstConfigInBlock.Plugin
		c.ListenHosts = c.firstConfigInBlock.ListenHosts
		c.Debug = c.firstConfigInBlock.Debug
		c.TLSConfig = c.firstConfigInBlock.TLSConfig
	}

	// we must map (group) each config to a bind address
	groups, err := groupConfigsByListenAddr(h.configs)
	if err != nil {
		return nil, err
	}
	// then we create a server for each group
	var servers []caddy.Server
	for addr, group := range groups {
		// switch on addr
		switch tr, _ := parse.Transport(addr); tr {
		case transport.DNS:
			s, err := NewServer(addr, group)
			if err != nil {
				return nil, err
			}
			servers = append(servers, s)

		case transport.TLS:
			s, err := NewServerTLS(addr, group)
			if err != nil {
				return nil, err
			}
			servers = append(servers, s)

		case transport.GRPC:
			s, err := NewServergRPC(addr, group)
			if err != nil {
				return nil, err
			}
			servers = append(servers, s)

		case transport.HTTPS:
			s, err := NewServerHTTPS(addr, group)
			if err != nil {
				return nil, err
			}
			servers = append(servers, s)
		}

	}

	return servers, nil
}

// AddPlugin adds a plugin to a site's plugin stack.
func (c *Config) AddPlugin(m plugin.Plugin) {
	c.Plugin = append(c.Plugin, m)
}

// registerHandler adds a handler to a site's handler registration. Handlers
//  use this to announce that they exist to other plugin.
func (c *Config) registerHandler(h plugin.Handler) {
	if c.registry == nil {
		c.registry = make(map[string]plugin.Handler)
	}

	// Just overwrite...
	c.registry[h.Name()] = h
}

// Handler returns the plugin handler that has been added to the config under its name.
// This is useful to inspect if a certain plugin is active in this server.
// Note that this is order dependent and the order is defined in directives.go, i.e. if your plugin
// comes before the plugin you are checking; it will not be there (yet).
func (c *Config) Handler(name string) plugin.Handler {
	if c.registry == nil {
		return nil
	}
	if h, ok := c.registry[name]; ok {
		return h
	}
	return nil
}

// Handlers returns a slice of plugins that have been registered. This can be used to
// inspect and interact with registered plugins but cannot be used to remove or add plugins.
// Note that this is order dependent and the order is defined in directives.go, i.e. if your plugin
// comes before the plugin you are checking; it will not be there (yet).
func (c *Config) Handlers() []plugin.Handler {
	if c.registry == nil {
		return nil
	}
	hs := make([]plugin.Handler, 0, len(c.registry))
	for k := range c.registry {
		hs = append(hs, c.registry[k])
	}
	return hs
}

func (h *dnsContext) validateZonesAndListeningAddresses() error {
	//Validate Zone and addresses
	checker := newOverlapZone()
	for _, conf := range h.configs {
		for _, h := range conf.ListenHosts {
			// Validate the overlapping of ZoneAddr
			akey := zoneAddr{Transport: conf.Transport, Zone: conf.Zone, Address: h, Port: conf.Port}
			existZone, overlapZone := checker.registerAndCheck(akey)
			if existZone != nil {
				return fmt.Errorf("cannot serve %s - it is already defined", akey.String())
			}
			if overlapZone != nil {
				return fmt.Errorf("cannot serve %s - zone overlap listener capacity with %v", akey.String(), overlapZone.String())
			}

		}
	}
	return nil

}

// groupSiteConfigsByListenAddr groups site configs by their listen
// (bind) address, so sites that use the same listener can be served
// on the same server instance. The return value maps the listen
// address (what you pass into net.Listen) to the list of site configs.
// This function does NOT vet the configs to ensure they are compatible.
func groupConfigsByListenAddr(configs []*Config) (map[string][]*Config, error) {
	groups := make(map[string][]*Config)
	for _, conf := range configs {
		for _, h := range conf.ListenHosts {
			addr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(h, conf.Port))
			if err != nil {
				return nil, err
			}
			addrstr := conf.Transport + "://" + addr.String()
			groups[addrstr] = append(groups[addrstr], conf)
		}
	}

	return groups, nil
}

// DefaultPort is the default port.
const DefaultPort = transport.Port

// These "soft defaults" are configurable by
// command line flags, etc.
var (
	// Port is the port we listen on by default.
	Port = DefaultPort

	// GracefulTimeout is the maximum duration of a graceful shutdown.
	GracefulTimeout time.Duration
)

var _ caddy.GracefulServer = new(Server)
