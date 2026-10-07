package httpserver

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

const adminSessionLifetime = 24 * time.Hour
const maxAdminSessions = 128

func adminBearer(req *http.Request) string {
	header := req.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(header, "Bearer ")
}

func (r *Relay) isAdminSecret(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(r.cfg.AdminSecret)) == 1
}

func adminUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	errorJSON(w, http.StatusUnauthorized, "admin authentication required")
}

func (r *Relay) authorized(w http.ResponseWriter, req *http.Request) bool {
	token := adminBearer(req)
	if r.isAdminSecret(token) {
		return true
	}
	digest := sha256.Sum256([]byte(token))
	r.mu.Lock()
	expires, exists := r.adminSessions[digest]
	valid := exists && time.Now().Before(expires)
	if exists && !valid {
		delete(r.adminSessions, digest)
	}
	r.mu.Unlock()
	if !valid {
		adminUnauthorized(w)
	}
	return valid
}

// A browser exchanges the deployment secret once, then uses a revocable,
// tab-scoped bearer token. Neither cookies nor proxy headers authenticate it.
func (r *Relay) adminLogin(w http.ResponseWriter, req *http.Request) {
	if !r.isAdminSecret(adminBearer(req)) {
		adminUnauthorized(w)
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		errorJSON(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	token := "ras_" + base64.RawURLEncoding.EncodeToString(b)
	now := time.Now()
	expires := now.Add(adminSessionLifetime)
	r.mu.Lock()
	for digest, expiry := range r.adminSessions {
		if !now.Before(expiry) {
			delete(r.adminSessions, digest)
		}
	}
	if len(r.adminSessions) >= maxAdminSessions {
		r.mu.Unlock()
		errorJSON(w, http.StatusTooManyRequests, "too many admin sessions")
		return
	}
	r.adminSessions[sha256.Sum256([]byte(token))] = expires
	r.mu.Unlock()
	writeJSON(w, struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}{token, expires})
}

func (r *Relay) adminLogout(w http.ResponseWriter, req *http.Request) {
	// Idempotent: an expired/revoked token is already signed out. Still require
	// an explicit bearer; cookies must never trigger even a logout operation.
	token := adminBearer(req)
	if token == "" {
		adminUnauthorized(w)
		return
	}
	r.mu.Lock()
	delete(r.adminSessions, sha256.Sum256([]byte(token)))
	r.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
