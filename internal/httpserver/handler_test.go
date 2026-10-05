package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHTTPContract(t *testing.T) {
	server := httptest.NewServer(NewHandler(BuildInfo{Version: "test", Commit: "abc123"}))
	defer server.Close()

	tests := []struct {
		name   string
		method string
		path   string
		status int
		body   map[string]string
	}{
		{"health", http.MethodGet, "/healthz", http.StatusOK, map[string]string{"status": "ok"}},
		{"version", http.MethodGet, "/version", http.StatusOK, map[string]string{"version": "test", "commit": "abc123"}},
		{"health head", http.MethodHead, "/healthz", http.StatusOK, nil},
		{"reject health write", http.MethodPost, "/healthz", http.StatusMethodNotAllowed, nil},
		{"reject version write", http.MethodPost, "/version", http.StatusMethodNotAllowed, nil},
		{"unknown route", http.MethodGet, "/", http.StatusNotFound, nil},
		{"no health subtree", http.MethodGet, "/healthz/private", http.StatusNotFound, nil},
		{"no tunnel placeholder", http.MethodGet, "/tunnel", http.StatusNotFound, nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, server.URL+test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.status)
			}
			if test.method == http.MethodHead {
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if len(body) != 0 {
					t.Fatalf("HEAD returned a body: %q", body)
				}
			}
			if test.body == nil {
				return
			}
			if got := response.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := response.Header.Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}
			var body map[string]string
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body, test.body) {
				t.Fatalf("body = %v, want %v", body, test.body)
			}
		})
	}
}
