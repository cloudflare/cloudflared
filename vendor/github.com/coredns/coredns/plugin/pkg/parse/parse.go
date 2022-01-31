// Package parse contains functions that can be used in the setup code for plugins.
package parse

import (
	"fmt"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin/pkg/transport"
)

// TransferIn parses transfer statements: 'transfer from [address...]'.
func TransferIn(c *caddy.Controller) (froms []string, err error) {
	if !c.NextArg() {
		return nil, c.ArgErr()
	}
	value := c.Val()
	switch value {
	default:
		return nil, c.Errf("unknown property %s", value)
	case "from":
		froms = c.RemainingArgs()
		if len(froms) == 0 {
			return nil, c.ArgErr()
		}
		for i := range froms {
			if froms[i] != "*" {
				normalized, err := HostPort(froms[i], transport.Port)
				if err != nil {
					return nil, err
				}
				froms[i] = normalized
			} else {
				return nil, fmt.Errorf("can't use '*' in transfer from")
			}
		}
	}
	return froms, nil
}
