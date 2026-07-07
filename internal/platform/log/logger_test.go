package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/campsite-booking/campsite-booking/internal/platform/config"
)

func TestNew_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := newWithWriter(&buf, config.LogConfig{Level: slog.LevelInfo, Format: config.LogJSON})

	logger.Info("hello", "key", "value")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("expected valid JSON output, got %q: %v", buf.String(), err)
	}
	if record["msg"] != "hello" {
		t.Errorf("expected msg=hello, got %v", record["msg"])
	}
}

func TestNew_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := newWithWriter(&buf, config.LogConfig{Level: slog.LevelInfo, Format: config.LogText})

	logger.Info("hello", "key", "value")

	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected non-JSON text output, got %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected output to contain the message, got %q", out)
	}
}

func TestNew_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := newWithWriter(&buf, config.LogConfig{Level: slog.LevelInfo, Format: config.LogText})

	logger.Debug("should be dropped")
	if buf.Len() != 0 {
		t.Errorf("expected debug record to be dropped at info level, got %q", buf.String())
	}

	logger.Info("should appear")
	if buf.Len() == 0 {
		t.Error("expected info record to be emitted at info level")
	}
}
