package k8s

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeK8sServer returns an httptest.Server that responds to /api/v1/services
// with the given service list.
func fakeK8sServer(t *testing.T, services serviceList) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(services); err != nil {
			t.Fatalf("failed to encode services: %v", err)
		}
	}))
}

func TestDiscoverServicesWithMockServer(t *testing.T) {
	log := zerolog.Nop()

	svcList := serviceList{
		Items: []serviceItem{
			{
				Metadata: objectMeta{
					Name:      "web",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationEnabled: "true",
					},
				},
				Spec: serviceSpec{
					ClusterIP: "10.96.0.1",
					Ports:     []servicePort{{Name: "http", Port: 80, Protocol: "TCP"}},
				},
			},
			{
				Metadata: objectMeta{
					Name:        "skipped",
					Namespace:   "default",
					Annotations: map[string]string{
						// No tunnel annotation
					},
				},
				Spec: serviceSpec{
					ClusterIP: "10.96.0.2",
					Ports:     []servicePort{{Port: 80}},
				},
			},
			{
				Metadata: objectMeta{
					Name:      "api",
					Namespace: "prod",
					Annotations: map[string]string{
						AnnotationEnabled:  "true",
						AnnotationHostname: "api.mycompany.com",
						AnnotationScheme:   "https",
						AnnotationPort:     "443",
					},
				},
				Spec: serviceSpec{
					ClusterIP: "10.96.1.5",
					Ports: []servicePort{
						{Name: "http", Port: 80},
						{Name: "https", Port: 443},
					},
				},
			},
		},
	}

	server := fakeK8sServer(t, svcList)
	defer server.Close()

	cfg := &Config{
		Enabled:    true,
		BaseDomain: "example.com",
	}

	// Override the client builder for testing.
	client := &kubeClient{
		baseURL:    server.URL,
		httpClient: server.Client(),
		log:        &log,
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	body, err := client.do(ctx, http.MethodGet, "/api/v1/services")
	require.NoError(t, err)

	var list serviceList
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Items, 3)

	// Now test the full discovery pipeline by filtering.
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
			continue
		}
		services = append(services, *si)
	}

	require.Len(t, services, 2)

	// web service
	assert.Equal(t, "web", services[0].Name)
	assert.Equal(t, "web-default.example.com", services[0].Hostname)
	assert.Equal(t, "http", services[0].Scheme)
	assert.Equal(t, int32(80), services[0].Port)

	// api service
	assert.Equal(t, "api", services[1].Name)
	assert.Equal(t, "api.mycompany.com", services[1].Hostname)
	assert.Equal(t, "https", services[1].Scheme)
	assert.Equal(t, int32(443), services[1].Port)
}

func TestWatcherServicesEqual(t *testing.T) {
	a := []ServiceInfo{
		{Name: "web", Namespace: "default", ClusterIP: "10.0.0.1", Port: 80, Scheme: "http", Hostname: "web.example.com"},
	}
	b := []ServiceInfo{
		{Name: "web", Namespace: "default", ClusterIP: "10.0.0.1", Port: 80, Scheme: "http", Hostname: "web.example.com"},
	}

	assert.True(t, servicesEqual(a, b))
	assert.True(t, servicesEqual(nil, nil))
	assert.False(t, servicesEqual(a, nil))
	assert.False(t, servicesEqual(nil, b))

	c := append(b, ServiceInfo{Name: "api", Namespace: "default", ClusterIP: "10.0.0.2", Port: 443, Scheme: "https", Hostname: "api.example.com"})
	assert.False(t, servicesEqual(a, c))
}
