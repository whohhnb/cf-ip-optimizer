package cryptox

import (
	"errors"
	"testing"
)

// A wrong secret must surface as ErrWrongKey so the caller can tell the user to
// re-enter credentials, instead of a bare 500 with no explanation.
func TestOpenWithWrongKey(t *testing.T) {
	oldBox, err := New("secret-from-before")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := oldBox.Seal(`{"id":"AKID","key":"xxx"}`)
	if err != nil {
		t.Fatal(err)
	}

	newBox, err := New("secret-after-someone-edited-env")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newBox.Open(sealed); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("expected ErrWrongKey, got %v", err)
	}

	// the original key still works
	plain, err := oldBox.Open(sealed)
	if err != nil {
		t.Fatalf("same key must decrypt: %v", err)
	}
	if plain != `{"id":"AKID","key":"xxx"}` {
		t.Fatalf("unexpected plaintext: %q", plain)
	}
}

func TestOpenEmptyAndGarbage(t *testing.T) {
	box, _ := New("k")
	if s, err := box.Open(""); err != nil || s != "" {
		t.Fatalf("empty ciphertext must be a no-op, got %q %v", s, err)
	}
	if _, err := box.Open("not-base64!!"); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("garbage must map to ErrWrongKey, got %v", err)
	}
}
