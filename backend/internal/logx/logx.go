// Package logx is the single logging entry point for the backend. Every call is
// written to stdout (for `docker logs` / journalctl) and, when a sink is
// registered, persisted for the in-app log viewer.
package logx

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

type Level string

const (
	Debug Level = "debug"
	Info  Level = "info"
	Warn  Level = "warn"
	Error Level = "error"
)

// Sink persists a log line. userID/domainID may be 0 for global events.
type Sink func(userID, domainID int64, level, msg string)

var (
	mu       sync.RWMutex
	sink     Sink
	minLevel = Info
)

// SetSink registers the persistence layer (usually the SQLite store).
func SetSink(s Sink) {
	mu.Lock()
	defer mu.Unlock()
	sink = s
}

// SetLevelFromString configures the console threshold (debug|info|warn|error).
func SetLevelFromString(v string) {
	mu.Lock()
	defer mu.Unlock()
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		minLevel = Debug
	case "warn":
		minLevel = Warn
	case "error":
		minLevel = Error
	default:
		minLevel = Info
	}
}

func rank(l Level) int {
	switch l {
	case Debug:
		return 0
	case Info:
		return 1
	case Warn:
		return 2
	default:
		return 3
	}
}

// Logf writes one line to the console and the sink.
func Logf(userID, domainID int64, level Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	mu.RLock()
	threshold := minLevel
	s := sink
	mu.RUnlock()

	if rank(level) >= rank(threshold) {
		log.Printf("[%-5s] %s", strings.ToUpper(string(level)), msg)
	}
	// debug lines are troubleshooting detail: console only, so the in-app
	// viewer stays focused on real events instead of scheduler chatter.
	if s != nil && rank(level) >= rank(Info) {
		s(userID, domainID, string(level), msg)
	}
}

// Log writes an already formatted message (no format expansion).
func Log(userID, domainID int64, level Level, msg string) {
	mu.RLock()
	threshold := minLevel
	s := sink
	mu.RUnlock()

	if rank(level) >= rank(threshold) {
		log.Printf("[%-5s] %s", strings.ToUpper(string(level)), msg)
	}
	if s != nil && rank(level) >= rank(Info) {
		s(userID, domainID, string(level), msg)
	}
}

// Consolef writes to stdout only. Used for high-volume or sensitive lines that
// should not land in the per-tenant log viewer.
func Consolef(level Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	mu.RLock()
	threshold := minLevel
	mu.RUnlock()
	if rank(level) >= rank(threshold) {
		log.Printf("[%-5s] %s", strings.ToUpper(string(level)), msg)
	}
}

func Debugf(userID, domainID int64, format string, args ...any) {
	Logf(userID, domainID, Debug, format, args...)
}
func Infof(userID, domainID int64, format string, args ...any) {
	Logf(userID, domainID, Info, format, args...)
}
func Warnf(userID, domainID int64, format string, args ...any) {
	Logf(userID, domainID, Warn, format, args...)
}
func Errorf(userID, domainID int64, format string, args ...any) {
	Logf(userID, domainID, Error, format, args...)
}

// Fatalf logs and exits, used for unrecoverable boot failures.
func Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[FATAL] %s", msg)
	os.Exit(1)
}
