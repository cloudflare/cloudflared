package metadata

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() { plugin.Register("metadata", setup) }

func setup(c *caddy.Controller) error {
	m, err := metadataParse(c)
	if err != nil {
		return err
	}
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		m.Next = next
		return m
	})

	c.OnStartup(func() error {
		plugins := dnsserver.GetConfig(c).Handlers()
		for _, p := range plugins {
			if met, ok := p.(Provider); ok {
				m.Providers = append(m.Providers, met)
			}
		}
		return nil
	})

	return nil
}

func metadataParse(c *caddy.Controller) (*Metadata, error) {
	m := &Metadata{}
	c.Next()

	m.Zones = plugin.OriginsFromArgsOrServerBlock(c.RemainingArgs(), c.ServerBlockKeys)

	if c.NextBlock() || c.Next() {
		return nil, plugin.Error("metadata", c.ArgErr())
	}
	return m, nil
}
