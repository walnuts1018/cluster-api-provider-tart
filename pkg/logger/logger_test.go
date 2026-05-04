package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestResolveLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info", input: "INFO", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "invalid falls back to info", input: "verbose", want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ResolveLevel(tt.input); got != tt.want {
				t.Fatalf("ResolveLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Type
	}{
		{name: "json", input: "json", want: TypeJSON},
		{name: "text", input: "TEXT", want: TypeText},
		{name: "invalid falls back to json", input: "pretty", want: TypeJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ResolveType(tt.input); got != tt.want {
				t.Fatalf("ResolveType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCreateJSONOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)
	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Fatalf("JSON output missing message field: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Fatalf("JSON output missing attribute: %s", output)
	}
}

func TestCreateTextOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)
	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Fatalf("Text output missing message: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Fatalf("Text output missing attribute: %s", output)
	}
}

func TestCreateDebugLevelEnablesSource(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true})
	logger := slog.New(handler)
	logger.Debug("debug message")

	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Fatalf("Debug output missing message: %s", output)
	}
}

func TestCreateWarnLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	logger := slog.New(handler)
	logger.Warn("warning message")

	output := buf.String()
	if !strings.Contains(output, `"level":"WARN"`) {
		t.Fatalf("Warn level output missing level field: %s", output)
	}
}

func TestCreateErrorLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})
	logger := slog.New(handler)
	logger.Error("error message")

	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Fatalf("Error level output missing level field: %s", output)
	}
}
