package httpapi

import "testing"

// The colo whitelist is user input that ends up in a filter comparison, so it
// must be normalized the same way everywhere: uppercase, no spaces, 3-letter
// IATA codes only.
func TestNormalizeColoAllow(t *testing.T) {
	ok := []struct{ in, want string }{
		{"", ""},
		{"hkg", "HKG"},
		{" hkg , nrt ", "HKG,NRT"},
		{"HKG,NRT,TPE,SIN", "HKG,NRT,TPE,SIN"},
	}
	for _, c := range ok {
		got, msg := normalizeColoAllow(c.in)
		if msg != "" {
			t.Fatalf("normalizeColoAllow(%q) rejected: %s", c.in, msg)
		}
		if got != c.want {
			t.Fatalf("normalizeColoAllow(%q)=%q, want %q", c.in, got, c.want)
		}
	}

	bad := []string{"HK", "HKGX", "HK1", "HKG,,NRT"}
	for _, in := range bad {
		if _, msg := normalizeColoAllow(in); msg == "" {
			t.Fatalf("normalizeColoAllow(%q) should be rejected", in)
		}
	}
}
