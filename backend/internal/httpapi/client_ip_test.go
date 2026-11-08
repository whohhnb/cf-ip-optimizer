package httpapi

import (
	"net/http/httptest"
	"testing"

	"edgepilot/internal/config"
)

// The client IP feeds login rate limiting and the audit log, so both the
// spoofing guard and the proxy handling need to be pinned down.
func TestClientIP(t *testing.T) {
	s := &Server{cfg: config.Config{TrustProxy: true}}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.9:51234"

	// single hop: Caddy overwrites the header with the real client
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := s.clientIP(req); got != "203.0.113.7" {
		t.Fatalf("single hop: got %q", got)
	}

	// nginx-style append: a spoofed value sits on the left, the real one is
	// appended on the right, so the rightmost entry must win
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.7")
	if got := s.clientIP(req); got != "203.0.113.7" {
		t.Fatalf("appended chain: got %q, want the rightmost entry", got)
	}

	// no header at all: fall back to the socket peer
	req.Header.Del("X-Forwarded-For")
	if got := s.clientIP(req); got != "10.0.0.9" {
		t.Fatalf("no header: got %q", got)
	}

	// trust disabled: the header must be ignored entirely, otherwise anyone
	// could rotate X-Forwarded-For to dodge the login limiter
	s.cfg.TrustProxy = false
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := s.clientIP(req); got != "10.0.0.9" {
		t.Fatalf("trust disabled must use RemoteAddr, got %q", got)
	}
}
