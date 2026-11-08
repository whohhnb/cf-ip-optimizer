package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Addr       string
	DataDir    string
	Secret     string // used for JWT signing + credential encryption
	BaseURL    string
	LogLevel   string
	TrustProxy bool
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func Load() Config {
	dataDir := env("PILOT_DATA_DIR", defaultDataDir())
	_ = os.MkdirAll(dataDir, 0o755)

	secret := env("PILOT_SECRET", "")
	if secret == "" {
		secret = loadOrCreateSecret(filepath.Join(dataDir, "secret.key"))
	}

	trust := false
	if v, err := strconv.ParseBool(env("PILOT_TRUST_PROXY", "false")); err == nil {
		trust = v
	}

	return Config{
		Addr:       env("PILOT_ADDR", ":8080"),
		DataDir:    dataDir,
		Secret:     secret,
		BaseURL:    strings.TrimRight(env("PILOT_BASE_URL", ""), "/"),
		LogLevel:   env("PILOT_LOG_LEVEL", "info"),
		TrustProxy: trust,
	}
}

// defaultDataDir stays next to the binary so nothing is ever written to the
// system drive. Override with PILOT_DATA_DIR (Docker uses /data).
func defaultDataDir() string {
	return "./data"
}

func loadOrCreateSecret(path string) string {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b))
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		copy(buf, []byte("edgepilot-insecure-fallback-secret-key-change-me"))
	}
	s := hex.EncodeToString(buf)
	_ = os.WriteFile(path, []byte(s), 0o600)
	return s
}
