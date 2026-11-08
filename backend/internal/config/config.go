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

	// EnvFile is the .env that was auto-loaded, empty when none was found.
	// EnvFileVars counts the variables it actually applied.
	EnvFile     string
	EnvFileVars int
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// loadDotEnv reads a KEY=VALUE file and sets any variable that is not already
// present in the real environment. Real environment variables always win, so
// `Environment=` in systemd or `-e` in docker still overrides the file.
func loadDotEnv(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	applied := 0
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if err := os.Setenv(key, val); err == nil {
			applied++
		}
	}
	return applied, true
}

// findDotEnv looks next to the working directory first (systemd sets it to the
// install dir), then next to the executable, which is where people usually drop
// the file when they start it by hand.
func findDotEnv() (string, int) {
	candidates := []string{".env"}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	seen := map[string]bool{}
	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		if n, ok := loadDotEnv(abs); ok {
			return abs, n
		}
	}
	return "", 0
}

func Load() Config {
	// 先加载 .env：它可能定义了 PILOT_DATA_DIR 等后续要读的变量
	envFile, envFileVars := findDotEnv()

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

		EnvFile:     envFile,
		EnvFileVars: envFileVars,
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
