package logger

import (
	"log/slog"
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
