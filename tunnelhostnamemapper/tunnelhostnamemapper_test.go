package tunnelhostnamemapper

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/originservice"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/stretchr/testify/assert"
)

const (
	routines = 1000
)

func TestTunnelHostnameMapperConcurrentAccess(t *testing.T) {
	thm := NewTunnelHostnameMapper()

	concurrentOps(t, func(i int) {
		// om is empty
		os, ok := thm.Get(tunnelHostname(i))
		assert.False(t, ok)
		assert.Nil(t, os)
	})

	firstURL, err := url.Parse("https://127.0.0.1:8080")
	assert.NoError(t, err)
	httpOS := originservice.NewHTTPService(http.DefaultTransport, firstURL, false)
	concurrentOps(t, func(i int) {
		thm.Add(tunnelHostname(i), httpOS)
	})

	concurrentOps(t, func(i int) {
		os, ok := thm.Get(tunnelHostname(i))
		assert.True(t, ok)
		assert.Equal(t, httpOS, os)
	})

	secondURL, err := url.Parse("https://127.0.0.1:8080")
	assert.NoError(t, err)
	secondHTTPOS := originservice.NewHTTPService(http.DefaultTransport, secondURL, true)
	concurrentOps(t, func(i int) {
		// Add should httpOS with secondHTTPOS
		thm.Add(tunnelHostname(i), secondHTTPOS)
	})

	concurrentOps(t, func(i int) {
		os, ok := thm.Get(tunnelHostname(i))
		assert.True(t, ok)
		assert.Equal(t, secondHTTPOS, os)
	})
}

func concurrentOps(t *testing.T, f func(i int)) {
	var wg sync.WaitGroup
	wg.Add(routines)
	for i := 0; i < routines; i++ {
		go func(i int) {
			f(i)
			wg.Done()
		}(i)
	}
	wg.Wait()
}

func tunnelHostname(i int) h2mux.TunnelHostname {
	return h2mux.TunnelHostname(fmt.Sprintf("%d.cftunnel.com", i))
}

func Test_toSet(t *testing.T) {

	type args struct {
		configs []*pogs.ReverseProxyConfig
	}
	tests := []struct {
		name string
		args args
		want map[h2mux.TunnelHostname]*pogs.ReverseProxyConfig
	}{
		{
			name: "empty slice should yield empty map",
			args: args{},
			want: map[h2mux.TunnelHostname]*pogs.ReverseProxyConfig{},
		},
		{
			name: "multiple elements",
			args: args{[]*pogs.ReverseProxyConfig{sampleConfig1(), sampleConfig2()}},
			want: map[h2mux.TunnelHostname]*pogs.ReverseProxyConfig{
				"mock.example.com":  sampleConfig1(),
				"mock2.example.com": sampleConfig2(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toSet(tt.args.configs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("toSet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTunnelHostnameMapper_ToAdd(t *testing.T) {
	type fields struct {
		tunnelHostnameToOrigin map[h2mux.TunnelHostname]originservice.OriginService
	}
	type args struct {
		newConfigs []*pogs.ReverseProxyConfig
	}
	tests := []struct {
		name      string
		fields    fields
		args      args
		wantToAdd []*pogs.ReverseProxyConfig
	}{
		{
			name: "Mapper={}, NewConfig={}, toAdd={}",
		},
		{
			name:      "Mapper={}, NewConfig={x}, toAdd={x}",
			args:      args{newConfigs: []*pogs.ReverseProxyConfig{sampleConfig1()}},
			wantToAdd: []*pogs.ReverseProxyConfig{sampleConfig1()},
		},
		{
			name:      "Mapper={x}, NewConfig={x,y}, toAdd={y}",
			args:      args{newConfigs: []*pogs.ReverseProxyConfig{sampleConfig2()}},
			wantToAdd: []*pogs.ReverseProxyConfig{sampleConfig2()},
			fields: fields{tunnelHostnameToOrigin: map[h2mux.TunnelHostname]originservice.OriginService{
				h2mux.TunnelHostname(sampleConfig1().TunnelHostname): &originservice.HelloWorldService{},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			om := &TunnelHostnameMapper{
				tunnelHostnameToOrigin: tt.fields.tunnelHostnameToOrigin,
			}
			if gotToAdd := om.ToAdd(tt.args.newConfigs); !reflect.DeepEqual(gotToAdd, tt.wantToAdd) {
				t.Errorf("TunnelHostnameMapper.ToAdd() = %v, want %v", gotToAdd, tt.wantToAdd)
			}
		})
	}
}

func TestTunnelHostnameMapper_ToRemove(t *testing.T) {
	type fields struct {
		tunnelHostnameToOrigin map[h2mux.TunnelHostname]originservice.OriginService
	}
	type args struct {
		newConfigs []*pogs.ReverseProxyConfig
	}
	tests := []struct {
		name         string
		fields       fields
		args         args
		wantToRemove []h2mux.TunnelHostname
	}{
		{
			name: "Mapper={}, NewConfig={}, toRemove={}",
		},
		{
			name:         "Mapper={x}, NewConfig={}, toRemove={x}",
			wantToRemove: []h2mux.TunnelHostname{sampleConfig1().TunnelHostname},
			fields: fields{tunnelHostnameToOrigin: map[h2mux.TunnelHostname]originservice.OriginService{
				h2mux.TunnelHostname(sampleConfig1().TunnelHostname): &originservice.HelloWorldService{},
			}},
		},
		{
			name: "Mapper={x}, NewConfig={x}, toRemove={}",
			args: args{newConfigs: []*pogs.ReverseProxyConfig{sampleConfig1()}},
			fields: fields{tunnelHostnameToOrigin: map[h2mux.TunnelHostname]originservice.OriginService{
				h2mux.TunnelHostname(sampleConfig1().TunnelHostname): &originservice.HelloWorldService{},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			om := &TunnelHostnameMapper{
				tunnelHostnameToOrigin: tt.fields.tunnelHostnameToOrigin,
			}
			if gotToRemove := om.ToRemove(tt.args.newConfigs); !reflect.DeepEqual(gotToRemove, tt.wantToRemove) {
				t.Errorf("TunnelHostnameMapper.ToRemove() = %v, want %v", gotToRemove, tt.wantToRemove)
			}
		})
	}
}

func sampleConfig1() *pogs.ReverseProxyConfig {
	return &pogs.ReverseProxyConfig{
		TunnelHostname:     "mock.example.com",
		OriginConfig:       &pogs.HelloWorldOriginConfig{},
		Retries:            18,
		ConnectionTimeout:  5 * time.Second,
		CompressionQuality: 3,
	}
}

func sampleConfig2() *pogs.ReverseProxyConfig {
	return &pogs.ReverseProxyConfig{
		TunnelHostname:     "mock2.example.com",
		OriginConfig:       &pogs.HelloWorldOriginConfig{},
		Retries:            18,
		ConnectionTimeout:  5 * time.Second,
		CompressionQuality: 3,
	}
}
