package httpserver

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func adminRequest(t *testing.T, server *httptest.Server, method, path, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// A normal proxy may rewrite host/port and terminate HTTPS upstream.
	req.Header.Set("Origin", "https://relay.example:8443")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "another.example")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func loginAdmin(t *testing.T, server *httptest.Server) string {
	t.Helper()
	before := time.Now()
	res := adminRequest(t, server, "POST", "/admin/api/session", strings.Repeat("a", 32), "")
	var session struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&session) != nil {
		t.Fatal("admin login failed", res.StatusCode)
	}
	if !strings.HasPrefix(session.Token, "ras_") || len(session.Token) != 47 || session.Token == strings.Repeat("a", 32) {
		t.Fatal("login must return an independent random token")
	}
	if session.ExpiresAt.Before(before.Add(adminSessionLifetime)) || session.ExpiresAt.After(time.Now().Add(adminSessionLifetime)) {
		t.Fatal("session expiry must be 24 hours from login")
	}
	if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("Set-Cookie") != "" || res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("session must not be cached, use cookies, or allow CORS")
	}
	return session.Token
}

func TestAdminSessionCRUDAndLogout(t *testing.T) {
	relay, server, desktopToken := testRelay(t)
	dialDesktop(t, server, desktopToken)
	token := loginAdmin(t, server)
	otherToken := loginAdmin(t, server)
	if token == otherToken {
		t.Fatal("logins must have independent tokens")
	}
	for _, operation := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/admin/api/desktops", "", 200},
		{"POST", "/admin/api/desktops", `{"desktopID":"new-desktop","label":"New"}`, 201},
		{"DELETE", "/admin/api/desktops/new-desktop", "", 204},
		{"DELETE", "/admin/api/session", "", 204},
		{"DELETE", "/admin/api/session", "", 204},
		{"GET", "/admin/api/desktops", "", 401},
		{"POST", "/admin/api/desktops", `{"desktopID":"intruder"}`, 401},
		{"DELETE", "/admin/api/desktops/desktop-one", "", 401},
	} {
		res := adminRequest(t, server, operation.method, operation.path, token, operation.body)
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != operation.status {
			t.Fatalf("%s %s = %d; want %d", operation.method, operation.path, res.StatusCode, operation.status)
		}
	}
	if adminRequest(t, server, "GET", "/admin/api/desktops", otherToken, "").StatusCode != 200 {
		t.Fatal("signing out must not revoke another login")
	}
	if !relay.cfg.Store.Authenticate("desktop-one", desktopToken) || len(relay.cfg.Store.List()) != 1 {
		t.Fatal("admin logout changed desktop credentials")
	}
	relay.mu.Lock()
	defer relay.mu.Unlock()
	if relay.tunnels["desktop-one"] == nil {
		t.Fatal("admin logout disconnected a desktop")
	}
	if _, exists := relay.adminSessions[sha256.Sum256([]byte(token))]; exists {
		t.Fatal("signed-out login remains valid")
	}
}

func TestAdminSessionExpiryAndRestart(t *testing.T) {
	relay, server, _ := testRelay(t)
	token := loginAdmin(t, server)
	relay.mu.Lock()
	relay.adminSessions[sha256.Sum256([]byte(token))] = time.Now().Add(-time.Second)
	relay.mu.Unlock()
	if adminRequest(t, server, "GET", "/admin/api/desktops", token, "").StatusCode != 401 {
		t.Fatal("expired token accepted")
	}
	token = loginAdmin(t, server)
	restarted, handler, err := NewRelay(BuildInfo{}, relay.cfg)
	if err != nil {
		t.Fatal(err)
	}
	restartServer := httptest.NewServer(handler)
	t.Cleanup(func() { restarted.Close(); restartServer.Close() })
	if adminRequest(t, restartServer, "GET", "/admin/api/desktops", token, "").StatusCode != 401 {
		t.Fatal("login survived relay restart")
	}
	loginAdmin(t, restartServer)
	relay.Close()
	if adminRequest(t, server, "GET", "/admin/api/desktops", token, "").StatusCode != 401 {
		t.Fatal("close did not discard login sessions")
	}
}

func TestAdminSessionAuthenticationBoundary(t *testing.T) {
	_, server, desktopToken := testRelay(t)
	token := loginAdmin(t, server)
	for _, credential := range []string{"", "wrong", desktopToken, token} {
		if adminRequest(t, server, "POST", "/admin/api/session", credential, "").StatusCode != 401 {
			t.Fatal("only the administrator secret may create login sessions")
		}
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "/admin/api/desktops"},
		{"POST", "/admin/api/session"},
		{"DELETE", "/admin/api/session"},
	} {
		req, _ := http.NewRequest(route.method, server.URL+route.path, nil)
		req.Header.Set("Cookie", "admin_session="+token+"; admin_secret="+strings.Repeat("a", 32))
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Fatal("cookie authentication permitted")
		}
	}
	for _, method := range []string{"POST", "DELETE"} {
		req, _ := http.NewRequest("OPTIONS", server.URL+"/admin/api/session", nil)
		req.Header.Set("Origin", "https://intruder.example")
		req.Header.Set("Access-Control-Request-Method", method)
		req.Header.Set("Access-Control-Request-Headers", "authorization")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 405 || res.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("cross-origin session preflight permitted")
		}
	}
}

func TestAdminSessionsBoundedAndExpiredEntriesPruned(t *testing.T) {
	relay, server, _ := testRelay(t)
	relay.mu.Lock()
	for i := 0; i < maxAdminSessions; i++ {
		relay.adminSessions[[32]byte{byte(i)}] = time.Now().Add(time.Hour)
	}
	relay.mu.Unlock()
	if adminRequest(t, server, "POST", "/admin/api/session", strings.Repeat("a", 32), "").StatusCode != 429 {
		t.Fatal("unbounded login sessions")
	}
	relay.mu.Lock()
	for digest := range relay.adminSessions {
		relay.adminSessions[digest] = time.Now().Add(-time.Second)
	}
	relay.mu.Unlock()
	loginAdmin(t, server)
	relay.mu.Lock()
	defer relay.mu.Unlock()
	if len(relay.adminSessions) != 1 {
		t.Fatal("expired sessions not pruned")
	}
}
