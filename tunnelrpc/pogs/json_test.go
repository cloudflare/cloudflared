package pogs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestScopeUnmarshaler_UnmarshalJSON(t *testing.T) {
	type fields struct {
		Scope Scope
	}
	type args struct {
		b []byte
	}
	tests := []struct {
		name      string
		fields    fields
		args      args
		wantErr   bool
		wantScope Scope
	}{
		{
			name:      "group_successful",
			args:      args{b: []byte(`{"group": "my-group"}`)},
			wantScope: NewGroup("my-group"),
		},
		{
			name:      "system_name_successful",
			args:      args{b: []byte(`{"system_name": "my-computer"}`)},
			wantScope: NewSystemName("my-computer"),
		},
		{
			name:    "not_a_scope",
			args:    args{b: []byte(`{"x": "y"}`)},
			wantErr: true,
		},
		{
			name:    "malformed_group",
			args:    args{b: []byte(`{"group": ["a", "b"]}`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			su := &ScopeUnmarshaler{
				Scope: tt.fields.Scope,
			}
			err := su.UnmarshalJSON(tt.args.b)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ScopeUnmarshaler.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				}
				if !eqScope(tt.wantScope, su.Scope) {
					t.Errorf("Wanted scope %v but got scope %v", tt.wantScope, su.Scope)
				}
			}
		})
	}
}

func TestUnmarshalOrigin(t *testing.T) {
	tests := []struct {
		jsonLiteral          string
		exceptedOriginConfig OriginConfig
	}{
		{
			jsonLiteral: `{
				"Http":{
					"url_string":"https.example.com",
					"tcp_keep_alive":7000000000,
					"dial_dual_stack":true,
					"tls_handshake_timeout":11000000000,
					"tls_verify":true,
					"origin_ca_pool":"/etc/cert.pem",
					"origin_server_name":"secure.example.com",
					"max_idle_connections":19,
					"idle_connection_timeout":17000000000,
					"proxy_connection_timeout":15000000000,
					"expect_continue_timeout":21000000000,
					"chunked_encoding":true
				}
			}`,
			exceptedOriginConfig: sampleHTTPOriginConfig(),
		},
		{
			jsonLiteral: `{
				"WebSocket":{
					"url_string":"ssh://example.com",
					"tls_verify":true,
					"origin_ca_pool":"/etc/cert.pem",
					"origin_server_name":"secure.example.com"
				}
			}`,
			exceptedOriginConfig: sampleWebSocketOriginConfig(),
		},
		{
			jsonLiteral: `{
				"HelloWorld": {}
			}`,
			exceptedOriginConfig: &HelloWorldOriginConfig{},
		},
	}

	for _, test := range tests {
		originConfigJSON := prettyToValidJSON(test.jsonLiteral)
		var OriginConfigJSONHandler OriginConfigJSONHandler
		err := json.Unmarshal([]byte(originConfigJSON), &OriginConfigJSONHandler)
		assert.NoError(t, err)
		assert.Equal(t, test.exceptedOriginConfig, OriginConfigJSONHandler.OriginConfig)
	}
}

func TestUnmarshalClientConfig(t *testing.T) {
	prettyClientConfigJSON := `{
		"version":10,
		"supervisor_config":{
			"auto_update_frequency":86400000000000,
			"metrics_update_frequency":300000000000,
			"grace_period":30000000000
		},
		"edge_connection_config":{
			"num_ha_connections":4,
			"heartbeat_interval":5000000000,
			"timeout":30000000000,
			"max_failed_heartbeats":5,
			"user_credential_path":"~/.cloudflared/cert.pem"
		},
		"doh_proxy_configs":[{
			"listen_host": "localhost",
			"listen_port": 53,
			"upstreams": ["https://1.1.1.1/dns-query", "https://1.0.0.1/dns-query"]
		}],
		"reverse_proxy_configs":[{
			"tunnel_hostname":"sdfjadk33.cftunnel.com",
			"origin_config":{
				"Http":{
					"url_string":"https://127.0.0.1:8080",
					"tcp_keep_alive":30000000000,
					"dial_dual_stack":true,
					"tls_handshake_timeout":10000000000,
					"tls_verify":true,
					"origin_ca_pool":"",
					"origin_server_name":"",
					"max_idle_connections":100,
					"idle_connection_timeout":90000000000,
					"proxy_connection_timeout":90000000000,
					"expect_continue_timeout":90000000000,
					"chunked_encoding":true
				}
			},
			"retries":5,
			"connection_timeout":30,
			"compression_quality":0
		}]
	}`
	// replace new line and tab
	clientConfigJSON := prettyToValidJSON(prettyClientConfigJSON)

	var clientConfig ClientConfig
	err := json.Unmarshal([]byte(clientConfigJSON), &clientConfig)
	assert.NoError(t, err)

	assert.Equal(t, Version(10), clientConfig.Version)

	supervisorConfig := SupervisorConfig{
		AutoUpdateFrequency:    time.Hour * 24,
		MetricsUpdateFrequency: time.Second * 300,
		GracePeriod:            time.Second * 30,
	}
	assert.Equal(t, supervisorConfig, *clientConfig.SupervisorConfig)

	edgeConnectionConfig := EdgeConnectionConfig{
		NumHAConnections:    4,
		HeartbeatInterval:   time.Second * 5,
		Timeout:             time.Second * 30,
		MaxFailedHeartbeats: 5,
		UserCredentialPath:  "~/.cloudflared/cert.pem",
	}
	assert.Equal(t, edgeConnectionConfig, *clientConfig.EdgeConnectionConfig)

	dohProxyConfig := DoHProxyConfig{
		ListenHost: "localhost",
		ListenPort: 53,
		Upstreams:  []string{"https://1.1.1.1/dns-query", "https://1.0.0.1/dns-query"},
	}

	assert.Len(t, clientConfig.DoHProxyConfigs, 1)
	assert.Equal(t, dohProxyConfig, *clientConfig.DoHProxyConfigs[0])

	reverseProxyConfig := ReverseProxyConfig{
		TunnelHostname: "sdfjadk33.cftunnel.com",
		OriginConfigJSONHandler: &OriginConfigJSONHandler{
			OriginConfig: &HTTPOriginConfig{
				URLString:              "https://127.0.0.1:8080",
				TCPKeepAlive:           time.Second * 30,
				DialDualStack:          true,
				TLSHandshakeTimeout:    time.Second * 10,
				TLSVerify:              true,
				OriginCAPool:           "",
				OriginServerName:       "",
				MaxIdleConnections:     100,
				IdleConnectionTimeout:  time.Second * 90,
				ProxyConnectionTimeout: time.Second * 90,
				ExpectContinueTimeout:  time.Second * 90,
				ChunkedEncoding:        true,
			},
		},
		Retries:            5,
		ConnectionTimeout:  30,
		CompressionQuality: 0,
	}

	assert.Len(t, clientConfig.ReverseProxyConfigs, 1)
	assert.Equal(t, reverseProxyConfig, *clientConfig.ReverseProxyConfigs[0])
}

func TestMarshalFallibleConfig(t *testing.T) {
	tests := []struct {
		fallibleConfig     FallibleConfig
		expctedJSONLiteral string
	}{
		{
			fallibleConfig: sampleSupervisorConfig(),
			expctedJSONLiteral: `{
				"supervisor_config":{
					"auto_update_frequency":75600000000000,
					"metrics_update_frequency":660000000000,
					"grace_period":31000000000
				}
			}`,
		},
		{
			fallibleConfig: sampleEdgeConnectionConfig(),
			expctedJSONLiteral: `{
				"edge_connection_config":{
					"num_ha_connections":49,
					"heartbeat_interval":5000000000,
					"timeout":9000000000,
					"max_failed_heartbeats":9001,
					"user_credential_path":"/Users/example/.cloudflared/cert.pem"
				}
			}`,
		},
		{
			fallibleConfig: sampleDoHProxyConfig(),
			expctedJSONLiteral: `{
				"doh_proxy_config":{
					"listen_host":"127.0.0.1",
					"listen_port":53,
					"upstreams":["1.1.1.1","1.0.0.1"]
				}
			}`,
		},
		{
			fallibleConfig: sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.OriginConfigJSONHandler = &OriginConfigJSONHandler{sampleHTTPOriginConfig()}
			}),
			expctedJSONLiteral: `{
				"reverse_proxy_config":{
					"tunnel_hostname":"mock-non-lb-tunnel.example.com",
					"origin_config":{
						"Http":{
							"url_string":"https.example.com",
							"tcp_keep_alive":7000000000,
							"dial_dual_stack":true,
							"tls_handshake_timeout":11000000000,
							"tls_verify":true,
							"origin_ca_pool":"/etc/cert.pem",
							"origin_server_name":"secure.example.com",
							"max_idle_connections":19,
							"idle_connection_timeout":17000000000,
							"proxy_connection_timeout":15000000000,
							"expect_continue_timeout":21000000000,
							"chunked_encoding":true
						}
					},
					"retries":18,
					"connection_timeout":5000000000,
					"compression_quality":3
				}
			}`,
		},
		{
			fallibleConfig: sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.OriginConfigJSONHandler = &OriginConfigJSONHandler{sampleWebSocketOriginConfig()}
			}),
			expctedJSONLiteral: `{
				"reverse_proxy_config":{
					"tunnel_hostname":"mock-non-lb-tunnel.example.com",
					"origin_config":{
						"WebSocket":{
							"url_string":"ssh://example.com",
							"tls_verify":true,
							"origin_ca_pool":"/etc/cert.pem",
							"origin_server_name":"secure.example.com"
						}
					},
					"retries":18,
					"connection_timeout":5000000000,
					"compression_quality":3
				}
			}`,
		},
		{
			fallibleConfig: sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.OriginConfigJSONHandler = &OriginConfigJSONHandler{&HelloWorldOriginConfig{}}
			}),
			expctedJSONLiteral: `{
				"reverse_proxy_config":{
					"tunnel_hostname":"mock-non-lb-tunnel.example.com",
					"origin_config":{
						"HelloWorld":{}
					},
					"retries":18,
					"connection_timeout":5000000000,
					"compression_quality":3
				}
			}`,
		},
	}

	for _, test := range tests {
		b, err := json.Marshal(test.fallibleConfig)
		assert.NoError(t, err)
		assert.Equal(t, prettyToValidJSON(test.expctedJSONLiteral), string(b))
	}

}

type prettyJSON string

func prettyToValidJSON(prettyJSON string) string {
	return strings.ReplaceAll(strings.ReplaceAll(prettyJSON, "\n", ""), "\t", "")
}

func eqScope(s1, s2 Scope) bool {
	return s1.Value() == s2.Value() && s1.PostgresType() == s2.PostgresType()
}
