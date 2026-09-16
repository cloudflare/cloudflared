package k8s

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
)

func TestGenerateIngressRules(t *testing.T) {
	log := zerolog.Nop()

	services := []ServiceInfo{
		{
			Name:      "web",
			Namespace: "default",
			ClusterIP: "10.96.0.1",
			Port:      80,
			Scheme:    "http",
			Hostname:  "web-default.example.com",
		},
		{
			Name:             "api",
			Namespace:        "prod",
			ClusterIP:        "10.96.0.2",
			Port:             443,
			Scheme:           "https",
			Hostname:         "api.example.com",
			NoTLSVerify:      true,
			OriginServerName: "api.internal",
		},
		{
			Name:      "docs",
			Namespace: "default",
			ClusterIP: "10.96.0.3",
			Port:      8080,
			Scheme:    "http",
			Hostname:  "docs.example.com",
			Path:      "/docs/.*",
		},
	}

	rules := GenerateIngressRules(services, &log)
	require.Len(t, rules, 3)

	// Check first rule
	assert.Equal(t, "web-default.example.com", rules[0].Hostname)
	assert.Equal(t, "http://10.96.0.1:80", rules[0].Service)
	assert.Empty(t, rules[0].Path)
	assert.Nil(t, rules[0].OriginRequest.NoTLSVerify)

	// Check second rule with TLS overrides
	assert.Equal(t, "api.example.com", rules[1].Hostname)
	assert.Equal(t, "https://10.96.0.2:443", rules[1].Service)
	require.NotNil(t, rules[1].OriginRequest.NoTLSVerify)
	assert.True(t, *rules[1].OriginRequest.NoTLSVerify)
	require.NotNil(t, rules[1].OriginRequest.OriginServerName)
	assert.Equal(t, "api.internal", *rules[1].OriginRequest.OriginServerName)

	// Check third rule with path
	assert.Equal(t, "docs.example.com", rules[2].Hostname)
	assert.Equal(t, "/docs/.*", rules[2].Path)
}

func TestMergeWithExistingRules(t *testing.T) {
	k8sRules := []config.UnvalidatedIngressRule{
		{Hostname: "k8s-svc.example.com", Service: "http://10.96.0.1:80"},
	}

	t.Run("empty existing rules", func(t *testing.T) {
		merged := MergeWithExistingRules(nil, k8sRules)
		require.Len(t, merged, 1)
		assert.Equal(t, "k8s-svc.example.com", merged[0].Hostname)
	})

	t.Run("empty k8s rules", func(t *testing.T) {
		existing := []config.UnvalidatedIngressRule{
			{Hostname: "www.example.com", Service: "http://localhost:8080"},
			{Service: "http_status:404"},
		}
		merged := MergeWithExistingRules(existing, nil)
		assert.Equal(t, existing, merged)
	})

	t.Run("merge with catch-all", func(t *testing.T) {
		existing := []config.UnvalidatedIngressRule{
			{Hostname: "www.example.com", Service: "http://localhost:8080"},
			{Service: "http_status:404"}, // catch-all
		}
		merged := MergeWithExistingRules(existing, k8sRules)
		require.Len(t, merged, 3)
		// User rule first
		assert.Equal(t, "www.example.com", merged[0].Hostname)
		// K8s rule
		assert.Equal(t, "k8s-svc.example.com", merged[1].Hostname)
		// Catch-all last
		assert.Equal(t, "http_status:404", merged[2].Service)
	})

	t.Run("no catch-all adds default", func(t *testing.T) {
		existing := []config.UnvalidatedIngressRule{
			{Hostname: "www.example.com", Service: "http://localhost:8080"},
		}
		merged := MergeWithExistingRules(existing, k8sRules)
		require.Len(t, merged, 3)
		// Should have a catch-all appended
		assert.Equal(t, "http_status:503", merged[2].Service)
	})

	t.Run("deduplication", func(t *testing.T) {
		existing := []config.UnvalidatedIngressRule{
			{Hostname: "k8s-svc.example.com", Service: "http://override:9090"},
			{Service: "http_status:404"},
		}
		merged := MergeWithExistingRules(existing, k8sRules)
		// K8s rule for k8s-svc.example.com should be deduplicated
		require.Len(t, merged, 2)
		// The user-defined one takes priority
		assert.Equal(t, "http://override:9090", merged[0].Service)
	})
}
