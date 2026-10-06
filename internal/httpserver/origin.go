package httpserver

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

func parseTrustedProxies(values []string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, errors.New("trusted proxies must be CIDR ranges")
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func canonicalOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || strings.ContainsAny(value, "?#") {
		return "", errors.New("invalid request origin")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}

func (r *Relay) requestOrigin(req *http.Request) (string, error) {
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	peer, err := netip.ParseAddrPort(req.RemoteAddr)
	trusted := false
	if err == nil {
		for _, prefix := range r.trustedProxies {
			if prefix.Contains(peer.Addr().Unmap()) {
				trusted = true
				break
			}
		}
	}
	if trusted {
		values := req.Header.Values("X-Forwarded-Proto")
		if len(values) != 0 {
			if len(values) != 1 || (values[0] != "http" && values[0] != "https") {
				return "", errors.New("invalid proxy protocol")
			}
			scheme = values[0]
		}
	}
	// Preserve Host; only an explicitly trusted peer may describe its TLS termination.
	// Forwarded and X-Forwarded-Host never select the request authority.
	return canonicalOrigin(scheme + "://" + req.Host)
}
