package httpserver

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminOriginFollowsHostAndTrustsOnlyExplicitProxyPeers(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	relay, handler, err := NewRelay(BuildInfo{}, Config{Store: store, AdminSecret: strings.Repeat("a", 32), TrustedProxies: []string{"192.0.2.1/32", "2001:db8::1/128"}})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	tests := []struct {
		name, host, origin, peer, proto, forwardedHost string
		nativeTLS, duplicateProto                      bool
		status                                         int
	}{
		{name: "HTTP bootstrap without domain config", host: "127.0.0.1:8080", origin: "http://127.0.0.1:8080", status: 200},
		{name: "first HTTPS proxy domain", host: "first.example", origin: "https://first.example", peer: "192.0.2.1:4567", proto: "https", status: 200},
		{name: "changed HTTPS domain on same running relay", host: "second.example", origin: "https://second.example", peer: "192.0.2.1:4567", proto: "https", status: 200},
		{name: "cross domain rejected", host: "second.example", origin: "https://first.example", peer: "192.0.2.1:4567", proto: "https", status: 403},
		{name: "cross scheme rejected", host: "second.example", origin: "http://second.example", peer: "192.0.2.1:4567", proto: "https", status: 403},
		{name: "cross port rejected", host: "second.example:8443", origin: "https://second.example", peer: "192.0.2.1:4567", proto: "https", status: 403},
		{name: "untrusted peer cannot claim TLS", host: "second.example", origin: "https://second.example", peer: "198.51.100.2:4567", proto: "https", status: 403},
		{name: "untrusted protocol header is ignored", host: "second.example", origin: "http://second.example", peer: "198.51.100.2:4567", proto: "https", status: 200},
		{name: "forwarded host is not an authority", host: "second.example", origin: "https://spoof.example", peer: "192.0.2.1:4567", proto: "https", forwardedHost: "spoof.example", status: 403},
		{name: "invalid trusted protocol rejected", host: "second.example", origin: "https://second.example", peer: "192.0.2.1:4567", proto: "https,http", status: 403},
		{name: "duplicate trusted protocol rejected", host: "second.example", origin: "https://second.example", peer: "192.0.2.1:4567", proto: "https", duplicateProto: true, status: 403},
		{name: "default HTTPS port and host case normalized", host: "SECOND.EXAMPLE:443", origin: "https://second.example", peer: "192.0.2.1:4567", proto: "https", status: 200},
		{name: "IPv6 proxy and host", host: "[2001:db8::2]:8443", origin: "https://[2001:db8::2]:8443", peer: "[2001:db8::1]:4567", proto: "https", status: 200},
		{name: "native TLS", host: "second.example", origin: "https://second.example", nativeTLS: true, status: 200},
		{name: "opaque origin rejected", host: "second.example", origin: "null", status: 403},
		{name: "malformed origin rejected", host: "second.example", origin: "http://fixture-user:fixture-secret@second.example", status: 403},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/admin/api/desktops", nil)
			req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
			req.Header.Set("Origin", tc.origin)
			if tc.peer != "" {
				req.RemoteAddr = tc.peer
			}
			if tc.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if tc.duplicateProto {
				req.Header.Add("X-Forwarded-Proto", "https")
			}
			req.Header.Set("X-Forwarded-Host", tc.forwardedHost)
			req.Header.Set("Forwarded", "proto=https;host=spoof.example")
			if tc.nativeTLS {
				req.TLS = &tls.ConnectionState{}
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status %d; want %d", response.Code, tc.status)
			}
		})
	}
}

func TestNoProxyTrustByDefault(t *testing.T) {
	relay, server, _ := testRelay(t)
	req := httptest.NewRequest(http.MethodGet, server.URL+"/admin/api/desktops", nil)
	req.RemoteAddr = "127.0.0.1:4567"
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	req.Header.Set("Origin", "https://"+req.Host)
	req.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	if relay.authorized(response, req) || response.Code != 403 {
		t.Fatal("unconfigured local proxy was trusted")
	}
}

func TestProxyCanChangeDomainsWithoutReconfiguringRelay(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	relay, handler, err := NewRelay(BuildInfo{}, Config{Store: store, AdminSecret: strings.Repeat("a", 32), TrustedProxies: []string{"127.0.0.1/32"}})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	backend := httptest.NewServer(handler)
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	front := httptest.NewTLSServer(proxy)
	defer front.Close()
	for _, domain := range []string{"first.example", "second.example"} {
		req, _ := http.NewRequest(http.MethodGet, front.URL+"/admin/api/desktops", nil)
		req.Host = domain
		req.Header.Set("Origin", "https://"+domain)
		req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
		resp, err := front.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("domain %s: %d", domain, resp.StatusCode)
		}
	}
}
