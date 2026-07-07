package config

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// requiredMappedKeys mirrors the design's env→field map (.env.example
// contract): every var LoadFrom reads must be documented there.
var requiredMappedKeys = []string{
	"APP_ENV",
	"HTTP_PORT",
	"HTTP_READ_TIMEOUT",
	"HTTP_WRITE_TIMEOUT",
	"HTTP_IDLE_TIMEOUT",
	"HTTP_SHUTDOWN_TIMEOUT",
	"DATABASE_URL",
	"SESSION_SECRET",
	"LOG_LEVEL",
	"LOG_FORMAT",
}

func parseEnvExample(t *testing.T) map[string]string {
	t.Helper()

	f, err := os.Open("../../../.env.example")
	if err != nil {
		t.Fatalf("failed to open .env.example: %v", err)
	}
	defer f.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("failed to read .env.example: %v", err)
	}
	return values
}

func TestEnvExample_DocumentsEveryMappedKeyAndLoads(t *testing.T) {
	values := parseEnvExample(t)

	for _, key := range requiredMappedKeys {
		if _, ok := values[key]; !ok {
			t.Errorf(".env.example is missing required key %q", key)
		}
	}

	cfg, err := LoadFrom(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("expected .env.example placeholder values to satisfy LoadFrom, got: %v", err)
	}
	if len(cfg.SessionSecret) < minSessionSecretLen {
		t.Errorf("expected .env.example SESSION_SECRET placeholder to be >= %d chars", minSessionSecretLen)
	}
}
