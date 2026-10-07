package httpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBrowserPagesAndAssetsUseEachDesktopTunnel(t *testing.T) {
	relay, server, token := testRelay(t)
	_, secondToken, err := relay.cfg.Store.Add("desktop-two", "Second desktop")
	if err != nil {
		t.Fatal(err)
	}
	first := dialDesktopForID(t, server, "desktop-one", token)
	second := dialDesktopForID(t, server, "desktop-two", secondToken)
	for _, test := range []struct {
		desktop, method, path, contentType, cache, body string
		status                                          int
	}{
		{"desktop-one", "GET", "/", "text/html; charset=utf-8", "no-store", "first desktop page", 200},
		{"desktop-one", "GET", "/pair", "text/html; charset=utf-8", "no-store", "first desktop pairing", 200},
		{"desktop-one", "GET", "/s/session", "text/html; charset=utf-8", "no-store", "first desktop deep link", 200},
		{"desktop-one", "GET", "/index.html", "text/html; charset=utf-8", "no-store", "first desktop index", 200},
		{"desktop-one", "GET", "/assets/app.js", "text/javascript; charset=utf-8", "public, max-age=3600", strings.Repeat("x", ChunkSize*2+17), 200},
		{"desktop-one", "GET", "/assets/app.css?theme=dark", "text/css; charset=utf-8", "public, max-age=3600", "desktop stylesheet", 200},
		{"desktop-one", "HEAD", "/assets/font.woff2", "font/woff2", "public, max-age=3600", "", 200},
		{"desktop-one", "GET", "/assets/missing.js", "application/json", "no-store", `{"error":"remote_asset_not_found"}`, 404},
		{"desktop-two", "GET", "/pair", "text/html; charset=utf-8", "no-store", "second desktop pairing", 200},
		{"desktop-two", "GET", "/assets/app.js", "text/javascript; charset=utf-8", "public, max-age=3600", "second desktop script", 200},
		// The same relay and live tunnel must immediately serve an updated desktop build.
		{"desktop-one", "GET", "/", "text/html; charset=utf-8", "no-store", "updated first desktop page", 200},
		{"desktop-one", "GET", "/assets/app.js", "text/javascript; charset=utf-8", "public, max-age=3600", "updated first desktop script", 200},
	} {
		t.Run(test.desktop+" "+test.method+" "+test.path+" "+test.body[:min(len(test.body), 24)], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type result struct {
				response *http.Response
				body     []byte
				err      error
			}
			finished := make(chan result, 1)
			go func() {
				request, _ := http.NewRequestWithContext(ctx, test.method, server.URL+"/d/"+test.desktop+test.path, nil)
				response, err := server.Client().Do(request)
				value := result{response: response, err: err}
				if err == nil {
					value.body, value.err = io.ReadAll(response.Body)
					response.Body.Close()
				}
				finished <- value
			}()
			connection := first
			if test.desktop == "desktop-two" {
				connection = second
			}
			request := readFrame(t, connection)
			for request.Type == "cancel" {
				request = readFrame(t, connection)
			}
			if request.Type != "request" || request.Method != test.method || request.Path != test.path {
				t.Fatalf("desktop request %#v", request)
			}
			if request.Headers["X-Pudding-Remote-Mode"] != "relay" {
				t.Fatal("desktop remote context missing")
			}
			end := readFrame(t, connection)
			if end.Type != "request_end" || end.ID != request.ID {
				t.Fatalf("request end %#v", end)
			}
			csp := "default-src 'self'; base-uri 'self'; frame-ancestors 'none'"
			writeFrame(t, connection, frame{Type: "response", ID: request.ID, Status: test.status, Headers: map[string]string{
				"Content-Type": test.contentType, "Cache-Control": test.cache,
				"Content-Security-Policy": csp, "X-Content-Type-Options": "nosniff",
			}})
			body := []byte(test.body)
			for offset := 0; offset < len(body); offset += ChunkSize {
				chunk := body[offset:min(offset+ChunkSize, len(body))]
				writeFrame(t, connection, frame{Type: "response_data", ID: request.ID, Data: base64.StdEncoding.EncodeToString(chunk)})
				ack := readFrame(t, connection)
				if ack.Type != "ack" || ack.ID != request.ID || ack.Direction != "response" {
					t.Fatalf("response ACK %#v", ack)
				}
			}
			writeFrame(t, connection, frame{Type: "response_end", ID: request.ID})
			value := <-finished
			if value.err != nil {
				t.Fatal(value.err)
			}
			if value.response.StatusCode != test.status || !bytes.Equal(value.body, body) {
				t.Fatalf("browser response %d %q", value.response.StatusCode, value.body)
			}
			for header, expected := range map[string]string{"Content-Type": test.contentType, "Cache-Control": test.cache, "Content-Security-Policy": csp, "X-Content-Type-Options": "nosniff"} {
				if got := value.response.Header.Get(header); got != expected {
					t.Fatalf("%s = %q, want desktop value %q", header, got, expected)
				}
			}
		})
		if t.Failed() {
			return
		}
	}
}

func TestBrowserRequestsRequireAnOnlineDesktop(t *testing.T) {
	_, server, _ := testRelay(t)
	for _, path := range []string{"/", "/pair", "/s/session", "/assets/app.js", "/remote/config", "/api/sessions"} {
		response, err := server.Client().Get(server.URL + "/d/desktop-one" + path)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]string
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if response.StatusCode != http.StatusServiceUnavailable || err != nil || payload["error"] != "desktop offline" {
			t.Fatalf("%s must report an offline desktop, got %d %#v %v", path, response.StatusCode, payload, err)
		}
	}
}
