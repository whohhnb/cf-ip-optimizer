package httpapi

import (
	"net/http/httptest"
	"testing"

	"edgepilot/internal/config"
)

func newIPServer(trust bool) *Server {
	return &Server{cfg: config.Config{
		TrustProxy:      trust,
		ClientIPHeaders: []string{"EO-Connecting-IP", "CF-Connecting-IP"},
	}}
}

// The client IP feeds login rate limiting and the audit log, so the header
// priority and the spoofing guard both need to be pinned down.
func TestClientIP(t *testing.T) {
	s := newIPServer(true)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.9:51234"

	// single proxy, no CDN: the rightmost XFF entry is the client
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := s.clientIP(req); got != "203.0.113.7" {
		t.Fatalf("single hop: got %q", got)
	}

	// nginx-style append: a spoofed value sits on the left, the real one is
	// appended on the right
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.7")
	if got := s.clientIP(req); got != "203.0.113.7" {
		t.Fatalf("appended chain: got %q", got)
	}

	// EdgeOne in front: XFF now ends with the CDN node, so the dedicated
	// header has to win or every visitor looks like the edge node
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 198.51.100.23")
	req.Header.Set("EO-Connecting-IP", "203.0.113.7")
	if got := s.clientIP(req); got != "203.0.113.7" {
		t.Fatalf("EdgeOne header must win over XFF, got %q", got)
	}

	// Cloudflare is the fallback CDN header
	req.Header.Del("EO-Connecting-IP")
	req.Header.Set("CF-Connecting-IP", "192.0.2.44")
	if got := s.clientIP(req); got != "192.0.2.44" {
		t.Fatalf("Cloudflare header: got %q", got)
	}

	// a garbage value must not be trusted just because the header exists
	req.Header.Set("CF-Connecting-IP", "not-an-ip")
	if got := s.clientIP(req); got != "198.51.100.23" {
		t.Fatalf("garbage CDN header must fall through to XFF, got %q", got)
	}

	// no headers at all: fall back to the socket peer
	req.Header.Del("X-Forwarded-For")
	req.Header.Del("CF-Connecting-IP")
	if got := s.clientIP(req); got != "10.0.0.9" {
		t.Fatalf("no header: got %q", got)
	}

	// trust disabled: every header is ignored, otherwise anyone could rotate
	// them to dodge the login limiter
	off := newIPServer(false)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("EO-Connecting-IP", "5.6.7.8")
	if got := off.clientIP(req); got != "10.0.0.9" {
		t.Fatalf("trust disabled must use RemoteAddr, got %q", got)
	}
}
