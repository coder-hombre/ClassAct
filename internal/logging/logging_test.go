package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewLogger_ReturnsNonNil(t *testing.T) {
	logger := NewLogger()
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}
}

func TestNewLogger_OutputsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	logger.Info("test message", "key", "value")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	if msg, ok := entry["msg"]; !ok || msg != "test message" {
		t.Errorf("expected msg='test message', got %v", entry["msg"])
	}
	if val, ok := entry["key"]; !ok || val != "value" {
		t.Errorf("expected key='value', got %v", entry["key"])
	}
}
