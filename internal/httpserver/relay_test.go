package httpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func testRelay(t *testing.T) (*Relay, *httptest.Server, string) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Add("desktop-one", "Test")
	if err != nil {
		t.Fatal(err)
	}
	relay, h, err := NewRelay(BuildInfo{}, Config{Store: store, AdminSecret: strings.Repeat("a", 32), PublicURL: "https://relay.example"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	t.Cleanup(func() { relay.Close(); server.Close() })
	return relay, server, token
}
func dialDesktop(t *testing.T, s *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(s.URL, "http:", "ws:", 1)+"/tunnel", &websocket.DialOptions{Subprotocols: []string{Protocol}})
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(65536)
	t.Cleanup(func() { _ = c.CloseNow() })
	writeFrame(t, c, frame{Type: "hello", Protocol: 1, DesktopID: "desktop-one", Token: token})
	f := readFrame(t, c)
	if f.Type != "hello" || f.Protocol != 1 || f.DesktopID != "desktop-one" {
		t.Fatalf("hello: %#v", f)
	}
	return c
}
func writeFrame(t *testing.T, c *websocket.Conn, f frame) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, _ := json.Marshal(f)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}
func readFrame(t *testing.T, c *websocket.Conn) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var f frame
	if typ != websocket.MessageText || json.Unmarshal(b, &f) != nil {
		t.Fatalf("invalid frame %s", b)
	}
	return f
}
func TestRegistryRestartDigestPermissionsConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "registry.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			_, _, err := s.Add(fmt.Sprintf("desktop-%d", i), "Name")
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	_, token, err := s.Add("desktop-one", "Name")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(token)) {
		t.Fatal("credential persisted")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file permission %o", info.Mode().Perm())
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if dir.Mode().Perm() != 0700 {
		t.Fatalf("directory permission %o", dir.Mode().Perm())
	}
	restart, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(restart.List()) != 13 || !restart.Authenticate("desktop-one", token) || restart.Authenticate("desktop-0", token) {
		t.Fatal("restart credential identity failure")
	}
	if err := restart.Delete("desktop-one"); err != nil {
		t.Fatal(err)
	}
	again, err := OpenStore(path)
	if err != nil || again.Authenticate("desktop-one", token) {
		t.Fatal("revocation not persistent")
	}
	for _, e := range again.List() {
		if e.Digest != "" {
			t.Fatal("digest leaked in public list")
		}
	}
}
func TestAdminAuthenticationCreateRevoke(t *testing.T) {
	r, s, token := testRelay(t)
	c := dialDesktop(t, s, token)
	req, _ := http.NewRequest("DELETE", s.URL+"/admin/api/desktops/desktop-one", nil)
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.Status)
	}
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	req.Header.Set("Origin", "https://spoof.example")
	resp, err = s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal(resp.Status)
	}
	req.Header.Set("Origin", "https://relay.example")
	resp, err = s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 || r.cfg.Store.Authenticate("desktop-one", token) {
		t.Fatal("revoke failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err = c.Read(ctx); err == nil {
		t.Fatal("tunnel still connected")
	}
	req, _ = http.NewRequest("POST", s.URL+"/admin/api/desktops", strings.NewReader(`{"desktopID":"new-desktop","label":"New"}`))
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	resp, err = s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var output struct {
		Token string `json:"token"`
	}
	if resp.StatusCode != 201 || json.NewDecoder(resp.Body).Decode(&output) != nil || !r.cfg.Store.Authenticate("new-desktop", output.Token) {
		t.Fatal("create failed")
	}
}
func TestTunnelForwardUploadSSEAndHeaderBoundary(t *testing.T) {
	_, server, token := testRelay(t)
	c := dialDesktop(t, server, token)
	payload := bytes.Repeat([]byte("x"), ChunkSize+17)
	result := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest("POST", server.URL+"/d/desktop-one/api/sessions/session/events?after=4", bytes.NewReader(payload))
		req.Header.Set("Cookie", "phone=secret")
		req.Header.Set("Origin", "https://relay.example")
		req.Header.Set("Last-Event-ID", "4")
		req.Header.Set("Authorization", "Bearer daemon-secret")
		req.Header.Set("X-Pudding-Remote-Origin", "https://spoof.example")
		req.Header.Set("X-Pudding-Remote-Mode", "direct")
		req.Header.Set("X-Forwarded-Host", "spoof.example")
		resp, err := server.Client().Do(req)
		if err == nil {
			defer resp.Body.Close()
			var b []byte
			b, err = io.ReadAll(resp.Body)
			if err == nil && (resp.StatusCode != 200 || string(b) != "id: 5\ndata: hello\n\n") {
				err = fmt.Errorf("response %d %q", resp.StatusCode, b)
			}
		}
		result <- err
	}()
	f := readFrame(t, c)
	if f.Type != "request" || f.Path != "/api/sessions/session/events?after=4" {
		t.Fatalf("request %#v", f)
	}
	if f.Headers["Cookie"] != "phone=secret" || f.Headers["Last-Event-Id"] != "4" || f.Headers["X-Pudding-Remote-Origin"] != "https://relay.example" || f.Headers["X-Pudding-Remote-Mode"] != "relay" || f.Headers["Authorization"] != "" || f.Headers["X-Forwarded-Host"] != "" {
		t.Fatalf("headers %#v", f.Headers)
	}
	id := f.ID
	var got []byte
	for {
		f = readFrame(t, c)
		if f.Type == "request_end" {
			break
		}
		if f.Type != "request_data" {
			t.Fatal(f.Type)
		}
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil || len(data) > ChunkSize {
			t.Fatal("chunk size")
		}
		got = append(got, data...)
		writeFrame(t, c, frame{Type: "ack", ID: id, Direction: "request"})
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("upload mismatch")
	}
	writeFrame(t, c, frame{Type: "response", ID: id, Status: 200, Headers: map[string]string{"Content-Type": "text/event-stream", "Set-Cookie": "phone=new; Secure; HttpOnly"}})
	writeFrame(t, c, frame{Type: "response_data", ID: id, Data: base64.StdEncoding.EncodeToString([]byte("id: 5\ndata: hello\n\n"))})
	ack := readFrame(t, c)
	if ack.Type != "ack" || ack.Direction != "response" {
		t.Fatal(ack)
	}
	writeFrame(t, c, frame{Type: "response_end", ID: id})
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
func TestStreamCancellationAndOffline(t *testing.T) {
	_, server, token := testRelay(t)
	resp, err := server.Client().Get(server.URL + "/d/desktop-one/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatal(resp.StatusCode)
	}
	c := dialDesktop(t, server, token)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/d/desktop-one/api/sessions/session/events", nil)
	done := make(chan struct{})
	go func() {
		resp, _ := server.Client().Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		close(done)
	}()
	f := readFrame(t, c)
	if f.Type != "request" {
		t.Fatal(f)
	}
	id := f.ID
	cancel()
	for {
		f = readFrame(t, c)
		if f.Type == "cancel" {
			break
		}
	}
	if f.ID != id {
		t.Fatal("wrong cancellation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation blocked")
	}
}
func TestInvalidHelloAndPublicURL(t *testing.T) {
	r, s, _ := testRelay(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(s.URL, "http:", "ws:", 1)+"/tunnel", &websocket.DialOptions{Subprotocols: []string{Protocol}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	writeFrame(t, c, frame{Type: "hello", Protocol: 2, DesktopID: "desktop-one", Token: strings.Repeat("x", 43)})
	if _, _, err = c.Read(ctx); err == nil {
		t.Fatal("invalid protocol accepted")
	}
	cfg := r.cfg
	for _, u := range []string{"http://relay.example", "https://relay.example/path", "https://user:pass@relay.example", "https://relay.example?secret=x"} {
		cfg.PublicURL = u
		if _, _, err := NewRelay(BuildInfo{}, cfg); err == nil {
			t.Fatal("accepted", u)
		}
	}
	cfg.PublicURL = "http://127.0.0.1:8080"
	cfg.AllowInsecureLoopback = true
	if relay, _, err := NewRelay(BuildInfo{}, cfg); err != nil {
		t.Fatal(err)
	} else {
		relay.Close()
	}
}
func TestBoundedStreamProtocol(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &stream{ctx: ctx, cancel: cancel, events: make(chan frame, 3), ack: make(chan struct{}, 1)}
	tun := &tunnel{streams: map[string]*stream{"one": s}}
	if !tun.dispatch(frame{Type: "response", ID: "one", Status: 200}) {
		t.Fatal("response rejected")
	}
	chunk := frame{Type: "response_data", ID: "one", Data: base64.StdEncoding.EncodeToString([]byte("x"))}
	if !tun.dispatch(chunk) || tun.dispatch(chunk) || tun.dispatch(frame{Type: "response_end", ID: "one"}) {
		t.Fatal("unacked chunk not bounded")
	}
	s.mu.Lock()
	s.responsePending = false
	s.mu.Unlock()
	if tun.dispatch(frame{Type: "response_data", ID: "one", Data: base64.StdEncoding.EncodeToString(make([]byte, ChunkSize+1))}) {
		t.Fatal("oversize chunk accepted")
	}
	if tun.dispatch(frame{Type: "ack", ID: "one", Direction: "request"}) {
		t.Fatal("unexpected ack accepted")
	}
}
func TestStaticAssetsAndDeepLinks(t *testing.T) {
	r, _, _ := testRelay(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("mobile app"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := r.cfg
	cfg.AssetsDir = dir
	relay, h, err := NewRelay(BuildInfo{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	for _, path := range []string{"/d/desktop-one/", "/d/desktop-one/pair", "/d/desktop-one/s/session"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.String() != "mobile app" {
			t.Fatalf("%s %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestDesktopStreamLimitAndGracefulClose(t *testing.T) {
	relay, server, token := testRelay(t)
	c := dialDesktop(t, server, token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < maxStreams; i++ {
		wg.Go(func() {
			req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/d/desktop-one/api/sessions", nil)
			resp, _ := server.Client().Do(req)
			if resp != nil {
				resp.Body.Close()
			}
		})
	}
	requests := 0
	for requests < maxStreams {
		if readFrame(t, c).Type == "request" {
			requests++
		}
	}
	resp, err := server.Client().Get(server.URL + "/d/desktop-one/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("stream limit status %d", resp.StatusCode)
	}
	relay.Close()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not wake streams")
	}
}

func TestOutboundFramesAreBounded(t *testing.T) {
	_, server, token := testRelay(t)
	_ = dialDesktop(t, server, token)
	req, _ := http.NewRequest("GET", server.URL+"/d/desktop-one/api/sessions", nil)
	req.Header.Set("X-Large", strings.Repeat("a", maxFrameBytes))
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("oversize metadata status %d", resp.StatusCode)
	}
}

func TestMobileBaseResolvesDeepLinkAssets(t *testing.T) {
	r, _, _ := testRelay(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	html := `<!doctype html><base href="__PUDDING_REMOTE_BASE__" /><script src="assets/app.js"></script>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("mobile script"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := r.cfg
	cfg.AssetsDir = dir
	relay, h, err := NewRelay(BuildInfo{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	server := httptest.NewServer(h)
	defer server.Close()
	for _, path := range []string{"/d/desktop-one/", "/d/desktop-one/pair", "/d/desktop-one/s/session", "/d/desktop-one/index.html"} {
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || !strings.Contains(string(data), `<base href="/d/desktop-one/" />`) || strings.Contains(string(data), "__PUDDING_REMOTE_BASE__") {
			t.Fatalf("%s %d %s", path, resp.StatusCode, data)
		}
		if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "base-uri 'self'") {
			t.Fatal("base CSP")
		}
		base, err := url.Parse(server.URL + "/d/desktop-one/")
		if err != nil {
			t.Fatal(err)
		}
		asset, _ := url.Parse("assets/app.js")
		assetResp, err := server.Client().Get(base.ResolveReference(asset).String())
		if err != nil {
			t.Fatal(err)
		}
		js, err := io.ReadAll(assetResp.Body)
		assetResp.Body.Close()
		if err != nil || assetResp.StatusCode != 200 || string(js) != "mobile script" {
			t.Fatalf("asset %d %s %v", assetResp.StatusCode, js, err)
		}
	}
}

type closeBlockedBody struct {
	io.ReadCloser
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *closeBlockedBody) Close() error {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.ReadCloser.Close()
}

func TestInFlightRequestACKAfterCancelOrEarlyResponseKeepsTunnel(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(fmt.Sprintf("early=%v", early), func(t *testing.T) {
			relay, _, token := testRelay(t)
			_, h, err := NewRelay(BuildInfo{}, relay.cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Hold HTTP cleanup after cancellation so the real wire exercises the existing-stream window.
			started, release := make(chan struct{}), make(chan struct{})
			wrapped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					r.Body = &closeBlockedBody{ReadCloser: r.Body, started: started, release: release}
				}
				h.ServeHTTP(w, r)
			}))
			t.Cleanup(wrapped.Close)
			c := dialDesktop(t, wrapped, token)
			result := make(chan error, 1)
			go func() {
				resp, err := wrapped.Client().Post(wrapped.URL+"/d/desktop-one/api/submit", "application/octet-stream", strings.NewReader("upload"))
				if err == nil {
					_, err = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				result <- err
			}()
			f := readFrame(t, c)
			if f.Type != "request" {
				t.Fatal(f)
			}
			id := f.ID
			f = readFrame(t, c)
			if f.Type != "request_data" {
				t.Fatal(f)
			}
			if early {
				writeFrame(t, c, frame{Type: "response", ID: id, Status: 413})
				writeFrame(t, c, frame{Type: "response_end", ID: id})
			} else {
				writeFrame(t, c, frame{Type: "cancel", ID: id})
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("HTTP cleanup did not begin")
			}
			writeFrame(t, c, frame{Type: "ack", ID: id, Direction: "request"})
			close(release)
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			for {
				f = readFrame(t, c)
				if f.Type == "cancel" && f.ID == id {
					break
				}
			}
			// An ACK already queued before cancellation may also arrive after removal.
			writeFrame(t, c, frame{Type: "ack", ID: id, Direction: "request"})
			next := make(chan error, 1)
			go func() {
				resp, err := wrapped.Client().Get(wrapped.URL + "/d/desktop-one/api/sessions")
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode != 204 {
						err = fmt.Errorf("status %d", resp.StatusCode)
					}
				}
				next <- err
			}()
			for {
				f = readFrame(t, c)
				if f.Type == "request" {
					break
				}
			}
			writeFrame(t, c, frame{Type: "response", ID: f.ID, Status: 204})
			writeFrame(t, c, frame{Type: "response_end", ID: f.ID})
			if err := <-next; err != nil {
				t.Fatal(err)
			}
		})
	}
}
