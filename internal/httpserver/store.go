package httpserver

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var desktopIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var ErrExists = errors.New("desktop already registered")

type Registration struct {
	DesktopID string    `json:"desktopID"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
	Digest    string    `json:"digest,omitempty"`
}

// Store persists only registration metadata and SHA-256 credential digests.
// A single process owns the registry; it must not be shared between replicas.
type Store struct {
	mu      sync.Mutex
	path    string
	entries map[string]Registration
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]Registration{}}
	if path == "" {
		return nil, errors.New("registration file required")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &s.entries); err != nil {
		return nil, err
	}
	if s.entries == nil {
		return nil, errors.New("invalid registration file")
	}
	for id, e := range s.entries {
		b, err := hex.DecodeString(e.Digest)
		if !desktopIDPattern.MatchString(id) || e.DesktopID != id || err != nil || len(b) != sha256.Size {
			return nil, errors.New("invalid registration")
		}
	}
	return s, nil
}
func (s *Store) persist(entries map[string]Registration) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".registrations-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.path)
}
func (s *Store) Add(id, label string) (Registration, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !desktopIDPattern.MatchString(id) || len(label) > 256 {
		return Registration{}, "", errors.New("invalid desktop registration")
	}
	if _, ok := s.entries[id]; ok {
		return Registration{}, "", ErrExists
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return Registration{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	digest := sha256.Sum256([]byte(token))
	e := Registration{DesktopID: id, Label: label, CreatedAt: time.Now().UTC(), Digest: hex.EncodeToString(digest[:])}
	next := s.copy()
	next[id] = e
	if err := s.persist(next); err != nil {
		return Registration{}, "", err
	}
	s.entries = next
	e.Digest = ""
	return e, token, nil
}
func (s *Store) copy() map[string]Registration {
	next := make(map[string]Registration, len(s.entries))
	for k, v := range s.entries {
		next[k] = v
	}
	return next
}
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.copy()
	delete(next, id)
	if err := s.persist(next); err != nil {
		return err
	}
	s.entries = next
	return nil
}
func (s *Store) List() []Registration {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Registration, 0, len(s.entries))
	for _, e := range s.entries {
		e.Digest = ""
		out = append(out, e)
	}
	return out
}
func (s *Store) Authenticate(id, token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok || len(token) != 43 {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	expected, err := hex.DecodeString(e.Digest)
	return err == nil && subtle.ConstantTimeCompare(expected, sum[:]) == 1
}
