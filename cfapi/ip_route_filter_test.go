package cfapi

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestNewIpRouteFilterFromCLINetworkFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantContains string
		wantExcludes string
	}{
		{
			name:         "subset flag encodes network_subset",
			args:         []string{"--filter-network-is-subset-of", "10.0.0.0/8"},
			wantContains: "network_subset=10.0.0.0%2F8",
			wantExcludes: "network_superset=",
		},
		{
			name:         "superset flag encodes network_superset",
			args:         []string{"--filter-network-is-superset-of", "10.0.0.0/24"},
			wantContains: "network_superset=10.0.0.0%2F24",
			wantExcludes: "network_subset=",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			flagSet := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
			for _, cliFlag := range IpRouteFilterFlags {
				require.NoError(t, cliFlag.Apply(flagSet))
			}
			require.NoError(t, flagSet.Parse(test.args))

			filter, err := NewIpRouteFilterFromCLI(cli.NewContext(cli.NewApp(), flagSet, nil))
			require.NoError(t, err)

			encoded := filter.Encode()
			require.Contains(t, encoded, test.wantContains)
			require.NotContains(t, encoded, test.wantExcludes)
		})
	}
}
