package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A .env next to the binary is the first thing people try; before this existed
// the file was silently ignored and the panel kept using defaults.
func TestLoadDotEnvAppliesValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := `# 注释行
PILOT_TEST_A=hello
export PILOT_TEST_B=quoted value
PILOT_TEST_C="with spaces"
PILOT_TEST_D='single'
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"PILOT_TEST_A", "PILOT_TEST_B", "PILOT_TEST_C", "PILOT_TEST_D"} {
		os.Unsetenv(k)
		t.Cleanup(func() { os.Unsetenv(k) })
	}

	n, ok := loadDotEnv(path)
	if !ok || n != 4 {
		t.Fatalf("expected 4 vars applied, got %d (ok=%v)", n, ok)
	}
	cases := map[string]string{
		"PILOT_TEST_A": "hello",
		"PILOT_TEST_B": "quoted value",
		"PILOT_TEST_C": "with spaces",
		"PILOT_TEST_D": "single",
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
}

// A real environment variable must win over the file, otherwise a stray .env
// could silently override what systemd or docker was told to use.
func TestLoadDotEnvDoesNotOverrideRealEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("PILOT_TEST_X=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PILOT_TEST_X", "from-env")

	n, ok := loadDotEnv(path)
	if !ok {
		t.Fatal("file should have been read")
	}
	if n != 0 {
		t.Fatalf("expected nothing applied, got %d", n)
	}
	if got := os.Getenv("PILOT_TEST_X"); got != "from-env" {
		t.Fatalf("real env must win, got %q", got)
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if _, ok := loadDotEnv(filepath.Join(t.TempDir(), "nope.env")); ok {
		t.Fatal("missing file must report not-found")
	}
}
