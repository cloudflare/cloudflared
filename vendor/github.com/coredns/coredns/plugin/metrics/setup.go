package metrics

import (
	"net"
	"runtime"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/coremain"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics/vars"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/plugin/pkg/uniq"
)

var (
	log      = clog.NewWithPlugin("prometheus")
	u        = uniq.New()
	registry = newReg()
)

func init() { plugin.Register("prometheus", setup) }

func setup(c *caddy.Controller) error {
	m, err := parse(c)
	if err != nil {
		return plugin.Error("prometheus", err)
	}
	m.Reg = registry.getOrSet(m.Addr, m.Reg)

	c.OnStartup(func() error { m.Reg = registry.getOrSet(m.Addr, m.Reg); u.Set(m.Addr, m.OnStartup); return nil })
	c.OnRestartFailed(func() error { m.Reg = registry.getOrSet(m.Addr, m.Reg); u.Set(m.Addr, m.OnStartup); return nil })

	c.OnStartup(func() error { return u.ForEach() })
	c.OnRestartFailed(func() error { return u.ForEach() })

	c.OnStartup(func() error {
		conf := dnsserver.GetConfig(c)
		for _, h := range conf.ListenHosts {
			addrstr := conf.Transport + "://" + net.JoinHostPort(h, conf.Port)
			for _, p := range conf.Handlers() {
				vars.PluginEnabled.WithLabelValues(addrstr, conf.Zone, p.Name()).Set(1)
			}
		}
		return nil
	})
	c.OnRestartFailed(func() error {
		conf := dnsserver.GetConfig(c)
		for _, h := range conf.ListenHosts {
			addrstr := conf.Transport + "://" + net.JoinHostPort(h, conf.Port)
			for _, p := range conf.Handlers() {
				vars.PluginEnabled.WithLabelValues(addrstr, conf.Zone, p.Name()).Set(1)
			}
		}
		return nil
	})

	c.OnRestart(m.OnRestart)
	c.OnRestart(func() error { vars.PluginEnabled.Reset(); return nil })
	c.OnFinalShutdown(m.OnFinalShutdown)

	// Initialize metrics.
	buildInfo.WithLabelValues(coremain.CoreVersion, coremain.GitCommit, runtime.Version()).Set(1)

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		m.Next = next
		return m
	})

	return nil
}

func parse(c *caddy.Controller) (*Metrics, error) {
	met := New(defaultAddr)

	i := 0
	for c.Next() {
		if i > 0 {
			return nil, plugin.ErrOnce
		}
		i++

		zones := plugin.OriginsFromArgsOrServerBlock(nil /* args */, c.ServerBlockKeys)
		for _, z := range zones {
			met.AddZone(z)
		}
		args := c.RemainingArgs()

		switch len(args) {
		case 0:
		case 1:
			met.Addr = args[0]
			_, _, e := net.SplitHostPort(met.Addr)
			if e != nil {
				return met, e
			}
		default:
			return met, c.ArgErr()
		}
	}
	return met, nil
}

// defaultAddr is the address the where the metrics are exported by default.
const defaultAddr = "localhost:9153"
