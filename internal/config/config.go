package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds process-wide settings from environment variables.
type Config struct {
	ListenAddr        string
	WebhookSecret     string
	PatternsFile      string
	AutoAcceptRegex   string
	MatchField        string
	LogLevel          string
	DebugApproval     bool
	DebugTimeout      time.Duration
	DebugUIListenAddr string
}

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	timeoutStr := getenv("DEBUG_TIMEOUT", "120s")
	duration, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return Config{}, fmt.Errorf("DEBUG_TIMEOUT %q: %w", timeoutStr, err)
	}

	c := Config{
		ListenAddr:        getenv("LISTEN_ADDR", ":8080"),
		WebhookSecret:     strings.TrimSpace(os.Getenv("WEBHOOK_SECRET")),
		PatternsFile:      strings.TrimSpace(os.Getenv("PATTERNS_FILE")),
		AutoAcceptRegex:   strings.TrimSpace(os.Getenv("AUTO_ACCEPT_REGEX")),
		MatchField:        getenv("MATCH_FIELD", "torrent"),
		LogLevel:          strings.ToLower(getenv("LOG_LEVEL", "info")),
		DebugApproval:     parseBool(os.Getenv("DEBUG_APPROVAL")),
		DebugTimeout:      duration,
		DebugUIListenAddr: getenv("DEBUG_UI_LISTEN_ADDR", "127.0.0.1:8091"),
	}

	switch c.LogLevel {
	case "info", "debug", "warn", "error":
	default:
		return Config{}, fmt.Errorf("unsupported LOG_LEVEL %q (use info, debug)", c.LogLevel)
	}

	if c.WebhookSecret == "" {
		return Config{}, fmt.Errorf("WEBHOOK_SECRET is required")
	}

	if !c.DebugApproval {
		if strings.TrimSpace(c.PatternsFile) == "" && strings.TrimSpace(c.AutoAcceptRegex) == "" {
			return Config{}, fmt.Errorf("set PATTERNS_FILE or AUTO_ACCEPT_REGEX for normal mode, or DEBUG_APPROVAL=true")
		}
	}

	return c, nil
}
