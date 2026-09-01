package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled config is always valid",
			cfg:  Config{Enabled: false},
		},
		{
			name:    "enabled without baseDomain fails",
			cfg:     Config{Enabled: true},
			wantErr: true,
			errMsg:  "baseDomain",
		},
		{
			name:    "exposeAPIServer without apiServerHostname fails",
			cfg:     Config{Enabled: true, BaseDomain: "example.com", ExposeAPIServer: true},
			wantErr: true,
			errMsg:  "apiServerHostname",
		},
		{
			name: "valid minimal config",
			cfg:  Config{Enabled: true, BaseDomain: "example.com"},
		},
		{
			name: "valid full config",
			cfg: Config{
				Enabled:           true,
				BaseDomain:        "example.com",
				Namespace:         "default",
				ExposeAPIServer:   true,
				APIServerHostname: "k8s.example.com",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSelectPort(t *testing.T) {
	ports := []servicePort{
		{Name: "http", Port: 80, Protocol: "TCP"},
		{Name: "https", Port: 443, Protocol: "TCP"},
		{Name: "grpc", Port: 9090, Protocol: "TCP"},
	}

	tests := []struct {
		name           string
		ports          []servicePort
		portAnnotation string
		wantPort       int32
		wantName       string
		wantErr        bool
	}{
		{
			name:     "no annotation selects first port",
			ports:    ports,
			wantPort: 80,
			wantName: "http",
		},
		{
			name:           "select by name",
			ports:          ports,
			portAnnotation: "https",
			wantPort:       443,
			wantName:       "https",
		},
		{
			name:           "select by number",
			ports:          ports,
			portAnnotation: "9090",
			wantPort:       9090,
			wantName:       "grpc",
		},
		{
			name:           "non-existent port name fails",
			ports:          ports,
			portAnnotation: "nonexistent",
			wantErr:        true,
		},
		{
			name:    "empty port list fails",
			ports:   nil,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			port, name, err := selectPort(tc.ports, tc.portAnnotation)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantPort, port)
				assert.Equal(t, tc.wantName, name)
			}
		})
	}
}

func TestServiceInfoFromItem(t *testing.T) {
	cfg := &Config{
		Enabled:    true,
		BaseDomain: "example.com",
	}

	t.Run("basic service", func(t *testing.T) {
		item := serviceItem{
			Metadata: objectMeta{
				Name:        "web",
				Namespace:   "default",
				Annotations: map[string]string{AnnotationEnabled: "true"},
			},
			Spec: serviceSpec{
				ClusterIP: "10.96.0.1",
				Ports:     []servicePort{{Name: "http", Port: 80, Protocol: "TCP"}},
			},
		}

		si, err := serviceInfoFromItem(item, cfg)
		require.NoError(t, err)
		assert.Equal(t, "web", si.Name)
		assert.Equal(t, "default", si.Namespace)
		assert.Equal(t, "10.96.0.1", si.ClusterIP)
		assert.Equal(t, int32(80), si.Port)
		assert.Equal(t, "http", si.Scheme)
		assert.Equal(t, "web-default.example.com", si.Hostname)
		assert.Equal(t, "http://10.96.0.1:80", si.OriginURL())
	})

	t.Run("service with custom hostname", func(t *testing.T) {
		item := serviceItem{
			Metadata: objectMeta{
				Name:      "api",
				Namespace: "prod",
				Annotations: map[string]string{
					AnnotationEnabled:  "true",
					AnnotationHostname: "api.mycompany.com",
				},
			},
			Spec: serviceSpec{
				ClusterIP: "10.96.0.2",
				Ports:     []servicePort{{Name: "https", Port: 443, Protocol: "TCP"}},
			},
		}

		si, err := serviceInfoFromItem(item, cfg)
		require.NoError(t, err)
		assert.Equal(t, "api.mycompany.com", si.Hostname)
		assert.Equal(t, "https", si.Scheme)
	})

	t.Run("headless service is rejected", func(t *testing.T) {
		item := serviceItem{
			Metadata: objectMeta{
				Name:        "headless",
				Namespace:   "default",
				Annotations: map[string]string{AnnotationEnabled: "true"},
			},
			Spec: serviceSpec{
				ClusterIP: "None",
				Ports:     []servicePort{{Port: 80}},
			},
		}
		_, err := serviceInfoFromItem(item, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "headless")
	})

	t.Run("custom port annotation", func(t *testing.T) {
		item := serviceItem{
			Metadata: objectMeta{
				Name:      "multi-port",
				Namespace: "default",
				Annotations: map[string]string{
					AnnotationEnabled: "true",
					AnnotationPort:    "grpc",
				},
			},
			Spec: serviceSpec{
				ClusterIP: "10.96.0.3",
				Ports: []servicePort{
					{Name: "http", Port: 80},
					{Name: "grpc", Port: 9090},
				},
			},
		}
		si, err := serviceInfoFromItem(item, cfg)
		require.NoError(t, err)
		assert.Equal(t, int32(9090), si.Port)
		assert.Equal(t, "grpc", si.PortName)
	})

	t.Run("no-tls-verify annotation", func(t *testing.T) {
		item := serviceItem{
			Metadata: objectMeta{
				Name:      "insecure",
				Namespace: "default",
				Annotations: map[string]string{
					AnnotationEnabled:     "true",
					AnnotationNoTLSVerify: "true",
					AnnotationScheme:      "https",
				},
			},
			Spec: serviceSpec{
				ClusterIP: "10.96.0.4",
				Ports:     []servicePort{{Port: 8443}},
			},
		}
		si, err := serviceInfoFromItem(item, cfg)
		require.NoError(t, err)
		assert.True(t, si.NoTLSVerify)
		assert.Equal(t, "https", si.Scheme)
	})
}

func TestIsTrue(t *testing.T) {
	for _, v := range []string{"true", "True", "TRUE", "1", "yes", "YES"} {
		assert.True(t, isTrue(v), "expected isTrue(%q) to be true", v)
	}
	for _, v := range []string{"false", "0", "no", "", "random"} {
		assert.False(t, isTrue(v), "expected isTrue(%q) to be false", v)
	}
}
