package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	yaml "gopkg.in/yaml.v2"
)

func TestConfigFileSettings(t *testing.T) {
	var (
		firstIngress = UnvalidatedIngressRule{
			Hostname: "tunnel1.example.com",
			Path:     "/id",
			Service:  "https://localhost:8000",
		}
		secondIngress = UnvalidatedIngressRule{
			Hostname: "*",
			Path:     "",
			Service:  "https://localhost:8001",
		}
		warpRouting = WarpRoutingConfig{
			Enabled: true,
		}
	)
	rawYAML := `
tunnel: config-file-test
ingress:
 - hostname: tunnel1.example.com
   path: /id
   service: https://localhost:8000
 - hostname: "*"
   service: https://localhost:8001
warp-routing: 
  enabled: true
retries: 5
grace-period: 30s
percentage: 3.14
hostname: example.com
tag:
 - test
 - central-1
counters:
 - 123
 - 456
`
	var config configFileSettings
	err := yaml.Unmarshal([]byte(rawYAML), &config)
	assert.NoError(t, err)

	assert.Equal(t, "config-file-test", config.TunnelID)
	assert.Equal(t, firstIngress, config.Ingress[0])
	assert.Equal(t, secondIngress, config.Ingress[1])
	assert.Equal(t, warpRouting, config.WarpRouting)

	retries, err := config.Int("retries")
	assert.NoError(t, err)
	assert.Equal(t, 5, retries)

	gracePeriod, err := config.Duration("grace-period")
	assert.NoError(t, err)
	assert.Equal(t, time.Second*30, gracePeriod)

	percentage, err := config.Float64("percentage")
	assert.NoError(t, err)
	assert.Equal(t, 3.14, percentage)

	hostname, err := config.String("hostname")
	assert.NoError(t, err)
	assert.Equal(t, "example.com", hostname)

	tags, err := config.StringSlice("tag")
	assert.NoError(t, err)
	assert.Equal(t, "test", tags[0])
	assert.Equal(t, "central-1", tags[1])

	counters, err := config.IntSlice("counters")
	assert.NoError(t, err)
	assert.Equal(t, 123, counters[0])
	assert.Equal(t, 456, counters[1])

}
