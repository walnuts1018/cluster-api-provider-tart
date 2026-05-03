package main

import (
	"log/slog"
	"testing"
)

func TestResolveLogLevel(t *testing.T) {
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveLogLevel(tt.input); got != tt.want {
				t.Fatalf("resolveLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveLogType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  LogType
	}{
		{name: "json", input: "json", want: LogTypeJSON},
		{name: "text", input: "TEXT", want: LogTypeText},
		{name: "invalid falls back to json", input: "pretty", want: LogTypeJSON},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveLogType(tt.input); got != tt.want {
				t.Fatalf("resolveLogType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
