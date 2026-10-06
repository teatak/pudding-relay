package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

const Protocol = "pudding-relay.v1"
const ChunkSize = 32768
const maxStreams = 64
const maxFrameBytes = 65536
const heartbeatInterval = 30 * time.Second
const heartbeatTimeout = 10 * time.Second

var errFrameTooLarge = errors.New("frame exceeds limit")

type Config struct {
	PublicURL             string
	AdminSecret           string
	Store                 *Store
	AssetsDir             string
	AllowInsecureLoopback bool
}
type Relay struct {
	cfg     Config
	origin  string
	mu      sync.Mutex
	tunnels map[string]*tunnel
	closed  bool
}
type frame struct {
	Type      string            `json:"type"`
	ID        string            `json:"id,omitempty"`
	Protocol  int               `json:"protocol,omitempty"`
	DesktopID string            `json:"desktopID,omitempty"`
	Token     string            `json:"token,omitempty"`
	Method    string            `json:"method,omitempty"`
	Path      string            `json:"path,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Status    int               `json:"status,omitempty"`
	Data      string            `json:"data,omitempty"`
	Direction string            `json:"direction,omitempty"`
}
type stream struct {
	ctx             context.Context
	cancel          context.CancelFunc
	events          chan frame
	ack             chan struct{}
	mu              sync.Mutex
	responseStarted bool
	responseEnded   bool
	responsePending bool
	requestPending  bool
}
type tunnel struct {
	relay     *Relay
	desktopID string
	conn      *websocket.Conn
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	streams   map[string]*stream
	writeMu   sync.Mutex
}

func NewRelay(build BuildInfo, cfg Config) (*Relay, http.Handler, error) {
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, nil, errors.New("public URL must be an HTTPS origin")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(cfg.AllowInsecureLoopback && local && u.Scheme == "http") {
		return nil, nil, errors.New("public URL requires HTTPS (HTTP loopback needs explicit opt-in)")
	}
	if len(cfg.AdminSecret) < 32 || cfg.Store == nil {
		return nil, nil, errors.New("admin secret (32+ bytes) and registration store required")
	}
	relay := &Relay{cfg: cfg, origin: u.Scheme + "://" + u.Host, tunnels: map[string]*tunnel{}}
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", NewHandler(build))
	mux.Handle("GET /version", NewHandler(build))
	mux.HandleFunc("GET /tunnel", relay.acceptTunnel)
	mux.HandleFunc("/d/{desktopID}/", relay.forward)
	mux.HandleFunc("GET /admin", relay.adminPage)
	mux.HandleFunc("GET /admin/api/desktops", relay.adminList)
	mux.HandleFunc("POST /admin/api/desktops", relay.adminAdd)
	mux.HandleFunc("DELETE /admin/api/desktops/{desktopID}", relay.adminDelete)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})
	// Validate before ServeMux can canonicalize dot segments into redirects.
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/d/") && !validRemotePath(req.URL) {
			errorJSON(w, http.StatusBadRequest, "invalid path")
			return
		}
		mux.ServeHTTP(w, req)
	})
	return relay, handler, nil
}
func (r *Relay) Close() {
	r.mu.Lock()
	r.closed = true
	ts := make([]*tunnel, 0, len(r.tunnels))
	for _, t := range r.tunnels {
		ts = append(ts, t)
	}
	r.mu.Unlock()
	for _, t := range ts {
		t.stop()
	}
}
func (t *tunnel) stop() {
	t.cancel()
	_ = t.conn.CloseNow()
	t.mu.Lock()
	for _, s := range t.streams {
		s.cancel()
	}
	t.mu.Unlock()
}
func (t *tunnel) send(f frame) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(t.ctx, 10*time.Second)
	defer cancel()
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(b) > maxFrameBytes {
		return errFrameTooLarge
	}
	return t.conn.Write(ctx, websocket.MessageText, b)
}
func (t *tunnel) heartbeat() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(t.ctx, heartbeatTimeout)
			err := t.conn.Ping(ctx)
			cancel()
			if err != nil {
				t.stop()
				return
			}
		}
	}
}

func (r *Relay) acceptTunnel(w http.ResponseWriter, req *http.Request) {
	c, err := websocket.Accept(w, req, &websocket.AcceptOptions{Subprotocols: []string{Protocol}})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(maxFrameBytes)
	if c.Subprotocol() != Protocol {
		_ = c.Close(websocket.StatusPolicyViolation, "protocol required")
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	typ, b, err := c.Read(ctx)
	cancel()
	var hello frame
	if err != nil || typ != websocket.MessageText || json.Unmarshal(b, &hello) != nil || hello.Type != "hello" || hello.Protocol != 1 || !r.cfg.Store.Authenticate(hello.DesktopID, hello.Token) {
		_ = c.Close(websocket.StatusPolicyViolation, "invalid hello")
		return
	}
	tc, stop := context.WithCancel(req.Context())
	t := &tunnel{relay: r, desktopID: hello.DesktopID, conn: c, ctx: tc, cancel: stop, streams: map[string]*stream{}}
	defer t.stop()
	r.mu.Lock()
	// Recheck under the routing lock so revoke cannot race authentication/attachment.
	if r.closed || !r.cfg.Store.Authenticate(hello.DesktopID, hello.Token) {
		r.mu.Unlock()
		return
	}
	if _, exists := r.tunnels[hello.DesktopID]; exists {
		r.mu.Unlock()
		_ = c.Close(websocket.StatusPolicyViolation, "desktop already online")
		return
	}
	r.tunnels[hello.DesktopID] = t
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.tunnels[hello.DesktopID] == t {
			delete(r.tunnels, hello.DesktopID)
		}
		r.mu.Unlock()
	}()
	if t.send(frame{Type: "hello", Protocol: 1, DesktopID: hello.DesktopID}) != nil {
		return
	}
	go t.heartbeat()
	for {
		typ, b, err = c.Read(tc)
		if err != nil {
			return
		}
		var f frame
		if typ != websocket.MessageText || json.Unmarshal(b, &f) != nil || !t.dispatch(f) {
			_ = c.Close(websocket.StatusPolicyViolation, "invalid stream frame")
			return
		}
	}
}
func (t *tunnel) dispatch(f frame) bool {
	if f.ID == "" || len(f.ID) > 128 {
		return false
	}
	t.mu.Lock()
	s := t.streams[f.ID]
	t.mu.Unlock()
	if s == nil {
		return true
	} // A canceled HTTP request may have frames already in flight.
	if f.Type == "cancel" {
		s.cancel()
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch f.Type {
	case "ack":
		if f.Direction != "request" || !s.requestPending {
			return false
		}
		s.requestPending = false
		select {
		case s.ack <- struct{}{}:
			return true
		default:
			return false
		}
	case "response":
		if s.responseStarted || f.Status < 200 || f.Status > 599 {
			return false
		}
		s.responseStarted = true
	case "response_data":
		if !s.responseStarted || s.responseEnded || s.responsePending {
			return false
		}
		b, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil || len(b) > ChunkSize || len(b) == 0 {
			return false
		}
		s.responsePending = true
	case "response_end":
		if !s.responseStarted || s.responseEnded || s.responsePending {
			return false
		}
		s.responseEnded = true
	default:
		return false
	}
	select {
	case s.events <- f:
		return true
	case <-s.ctx.Done():
		return true
	default:
		return false
	}
}
func errorJSON(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
func cleanHeaders(in http.Header) map[string]string {
	out := map[string]string{}
	blocked := map[string]bool{"host": true, "connection": true, "keep-alive": true, "proxy-authenticate": true, "proxy-authorization": true, "te": true, "trailer": true, "transfer-encoding": true, "upgrade": true, "authorization": true, "content-length": true, "forwarded": true}
	for _, v := range in.Values("Connection") {
		for _, k := range strings.Split(v, ",") {
			blocked[strings.ToLower(strings.TrimSpace(k))] = true
		}
	}
	for k, v := range in {
		low := strings.ToLower(k)
		if blocked[low] || strings.HasPrefix(low, "x-pudding-") || strings.HasPrefix(low, "x-forwarded-") {
			continue
		}
		out[http.CanonicalHeaderKey(k)] = strings.Join(v, ", ")
	}
	return out
}
func validRemotePath(u *url.URL) bool {
	if strings.Contains(u.Path, "\\") || !utf8.ValidString(u.Path) {
		return false
	}
	escaped := strings.ToLower(u.EscapedPath())
	for _, forbidden := range []string{"%2f", "%5c", "%25", "%00"} {
		if strings.Contains(escaped, forbidden) {
			return false
		}
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (r *Relay) forward(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("desktopID")
	if !desktopIDPattern.MatchString(id) {
		http.NotFound(w, req)
		return
	}
	prefix := "/d/" + id
	path := strings.TrimPrefix(req.URL.Path, prefix)
	if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/remote/") {
		r.assets(w, req, prefix, path)
		return
	}
	// Preserve browser encoding (notably request IDs containing %3A) on the
	// wire; the desktop gateway and core decode each segment at their boundary.
	path = "/" + strings.SplitN(req.URL.EscapedPath(), "/", 4)[3]
	r.mu.Lock()
	t := r.tunnels[id]
	r.mu.Unlock()
	if t == nil {
		errorJSON(w, 503, "desktop offline")
		return
	}
	rid := make([]byte, 16)
	if _, err := rand.Read(rid); err != nil {
		errorJSON(w, 500, "request unavailable")
		return
	}
	requestID := base64.RawURLEncoding.EncodeToString(rid)
	ctx, cancel := context.WithCancel(req.Context())
	s := &stream{ctx: ctx, cancel: cancel, events: make(chan frame, 3), ack: make(chan struct{}, 1)}
	defer cancel()
	t.mu.Lock()
	if len(t.streams) >= maxStreams {
		t.mu.Unlock()
		errorJSON(w, 503, "desktop stream limit")
		return
	}
	t.streams[requestID] = s
	t.mu.Unlock()
	defer func() {
		cancel()
		_ = req.Body.Close()
		t.mu.Lock()
		delete(t.streams, requestID)
		t.mu.Unlock()
		_ = t.send(frame{Type: "cancel", ID: requestID})
	}()
	// HTTP/1 must allow an early response while the upload goroutine reads the body.
	_ = http.NewResponseController(w).EnableFullDuplex()
	headers := cleanHeaders(req.Header)
	headers["X-Pudding-Remote-Origin"] = r.origin
	headers["X-Pudding-Remote-Mode"] = "relay"
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}
	if err := t.send(frame{Type: "request", ID: requestID, Method: req.Method, Path: path, Headers: headers}); err != nil {
		if errors.Is(err, errFrameTooLarge) {
			errorJSON(w, http.StatusRequestHeaderFieldsTooLarge, "request metadata too large")
			return
		}
		errorJSON(w, 503, "desktop offline")
		return
	}
	go func() {
		buf := make([]byte, ChunkSize)
		for {
			n, err := req.Body.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.requestPending = true
				s.mu.Unlock()
				if t.send(frame{Type: "request_data", ID: requestID, Data: base64.StdEncoding.EncodeToString(buf[:n])}) != nil {
					cancel()
					return
				}
				select {
				case <-s.ack:
				case <-ctx.Done():
					return
				case <-t.ctx.Done():
					cancel()
					return
				}
			}
			if err == io.EOF {
				if t.send(frame{Type: "request_end", ID: requestID}) != nil {
					cancel()
				}
				return
			}
			if err != nil {
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
	started := false
	for {
		select {
		case <-ctx.Done():
			if !started {
				errorJSON(w, 502, "desktop request canceled")
			}
			return
		case <-t.ctx.Done():
			if !started {
				errorJSON(w, 503, "desktop offline")
			}
			return
		case f := <-s.events:
			switch f.Type {
			case "response":
				for k, v := range cleanHeadersFromMap(f.Headers) {
					w.Header().Set(k, v)
				}
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(f.Status)
				started = true
				if fl, ok := w.(http.Flusher); ok {
					fl.Flush()
				}
			case "response_data":
				b, _ := base64.StdEncoding.DecodeString(f.Data)
				if _, err := w.Write(b); err != nil {
					return
				}
				if fl, ok := w.(http.Flusher); ok {
					fl.Flush()
				}
				s.mu.Lock()
				s.responsePending = false
				s.mu.Unlock()
				if t.send(frame{Type: "ack", ID: requestID, Direction: "response"}) != nil {
					return
				}
			case "response_end":
				return
			}
		}
	}
}
func cleanHeadersFromMap(m map[string]string) map[string]string {
	h := http.Header{}
	for k, v := range m {
		h.Set(k, v)
	}
	return cleanHeaders(h)
}
func (r *Relay) assets(w http.ResponseWriter, req *http.Request, prefix, path string) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.NotFound(w, req)
		return
	}
	r.mu.Lock()
	registered := false
	for _, e := range r.cfg.Store.List() {
		if e.DesktopID == req.PathValue("desktopID") {
			registered = true
			break
		}
	}
	r.mu.Unlock()
	if !registered {
		http.NotFound(w, req)
		return
	}
	_, assetsErr := os.Stat(filepath.Join(r.cfg.AssetsDir, "index.html"))
	if r.cfg.AssetsDir == "" || assetsErr != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "<!doctype html><html lang=en><meta charset=utf-8><title>Pudding setup</title><h1>Mobile Web assets required / 需要手机 Web 资源</h1><p>Install the Pudding mobile Web build and set --assets-dir. / 请安装 Pudding 手机 Web 构建并设置 --assets-dir。</p>")
		return
	}
	if path == "/" || path == "/index.html" || path == "/pair" || strings.HasPrefix(path, "/s/") {
		data, err := os.ReadFile(filepath.Join(r.cfg.AssetsDir, "index.html"))
		if err != nil {
			errorJSON(w, http.StatusServiceUnavailable, "mobile assets unavailable")
			return
		}
		html := strings.ReplaceAll(string(data), "__PUDDING_REMOTE_BASE__", prefix+"/")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if req.Method != http.MethodHead {
			_, _ = io.WriteString(w, html)
		}
		return
	}
	http.StripPrefix(prefix, http.FileServer(http.Dir(r.cfg.AssetsDir))).ServeHTTP(w, req)
}
func (r *Relay) authorized(w http.ResponseWriter, req *http.Request) bool {
	header := req.Header.Get("Authorization")
	a := strings.TrimPrefix(header, "Bearer ")
	if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare([]byte(a), []byte(r.cfg.AdminSecret)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		errorJSON(w, 401, "admin authentication required")
		return false
	}
	if origin := req.Header.Get("Origin"); origin != "" && origin != r.origin {
		errorJSON(w, 403, "origin rejected")
		return false
	}
	return true
}
func (r *Relay) adminList(w http.ResponseWriter, req *http.Request) {
	if !r.authorized(w, req) {
		return
	}
	type entry struct {
		Registration
		Online bool `json:"online"`
	}
	list := r.cfg.Store.List()
	sort.Slice(list, func(i, j int) bool { return list[i].DesktopID < list[j].DesktopID })
	out := make([]entry, 0, len(list))
	r.mu.Lock()
	for _, e := range list {
		out = append(out, entry{e, r.tunnels[e.DesktopID] != nil})
	}
	r.mu.Unlock()
	writeJSON(w, map[string]any{"desktops": out})
}
func (r *Relay) adminAdd(w http.ResponseWriter, req *http.Request) {
	if !r.authorized(w, req) {
		return
	}
	var input struct {
		DesktopID string `json:"desktopID"`
		Label     string `json:"label"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || !desktopIDPattern.MatchString(input.DesktopID) || len(input.Label) > 256 {
		errorJSON(w, 400, "invalid registration")
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		errorJSON(w, 400, "invalid registration")
		return
	}
	e, token, err := r.cfg.Store.Add(input.DesktopID, input.Label)
	if errors.Is(err, ErrExists) {
		errorJSON(w, 409, "desktop already registered")
		return
	}
	if err != nil {
		errorJSON(w, 500, "registration failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(struct {
		Registration
		Token string `json:"token"`
	}{e, token})
}
func (r *Relay) adminDelete(w http.ResponseWriter, req *http.Request) {
	if !r.authorized(w, req) {
		return
	}
	id := req.PathValue("desktopID")
	r.mu.Lock()
	if err := r.cfg.Store.Delete(id); err != nil {
		r.mu.Unlock()
		errorJSON(w, 500, "revocation failed")
		return
	}
	t := r.tunnels[id]
	delete(r.tunnels, id)
	r.mu.Unlock()
	if t != nil {
		t.stop()
	}
	w.WriteHeader(204)
}
