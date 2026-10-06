package httpserver

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminBearerBehindStandardProxy(t *testing.T) {
	for _, origin := range []string{"http://relay.example:9623", "https://relay.example:8443", "https://new.example:9443"} {
		t.Run(origin, func(t *testing.T) {
			store, err := OpenStore(filepath.Join(t.TempDir(), "registry.json"))
			if err != nil {
				t.Fatal(err)
			}
			secret := strings.Repeat("a", 32)
			relay, handler, err := NewRelay(BuildInfo{}, Config{Store: store, AdminSecret: secret})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(relay.Close)
			req := httptest.NewRequest("POST", "http://relay.example/admin/api/desktops", strings.NewReader(`{"desktopID":"desktop_proxy","label":"Proxy"}`))
			req.Header.Set("Authorization", "Bearer "+secret)
			req.Header.Set("Origin", origin)
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-Forwarded-Host", "untrusted.example")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != 201 {
				t.Fatalf("status %d; want 201", response.Code)
			}
			if len(store.List()) != 1 {
				t.Fatal("registration missing")
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "" || response.Header().Get("Set-Cookie") != "" {
				t.Fatal("admin must not enable CORS or cookie authentication")
			}
		})
	}
}

func TestAdminRequiresBearerOnEveryOperation(t *testing.T) {
	for _, route := range []struct{ method, path string }{{"GET", "/admin/api/desktops"}, {"POST", "/admin/api/desktops"}, {"DELETE", "/admin/api/desktops/desktop-one"}} {
		for _, auth := range []string{"", "Bearer wrong", "Basic aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
			t.Run(route.method+" "+auth, func(t *testing.T) {
				relay, server, token := testRelay(t)
				req, err := http.NewRequest(route.method, server.URL+route.path, strings.NewReader(`{"desktopID":"intruder"}`))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", auth)
				req.Header.Set("Cookie", "admin_secret="+strings.Repeat("a", 32))
				req.Header.Set("Origin", "https://intruder.example")
				req.Header.Set("X-Forwarded-Proto", "https")
				response, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != 401 {
					t.Fatal("admin bearer authentication bypassed")
				}
				if len(relay.cfg.Store.List()) != 1 || !relay.cfg.Store.Authenticate("desktop-one", token) {
					t.Fatal("unauthorized request changed registration")
				}
			})
		}
	}
}

func TestAdminRejectsCrossOriginPreflight(t *testing.T) {
	_, server, _ := testRelay(t)
	req, _ := http.NewRequest("OPTIONS", server.URL+"/admin/api/desktops", nil)
	req.Header.Set("Origin", "https://intruder.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 405 || res.Header.Get("Access-Control-Allow-Origin") != "" || res.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("cross-origin admin preflight permitted")
	}
}
