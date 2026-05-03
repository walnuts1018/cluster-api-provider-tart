package logger

import (
	"log/slog"
	"os"
	"strings"
)

type Type string

const (
	TypeJSON Type = "json"
	TypeText Type = "text"
)

func Create(logLevelStr, logTypeStr string) *slog.Logger {
	logLevel := ResolveLevel(logLevelStr)
	logType := ResolveType(logTypeStr)

	options := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: logLevel == slog.LevelDebug,
	}

	var handler slog.Handler
	switch logType {
	case TypeText:
		handler = slog.NewTextHandler(os.Stdout, options)
	default:
		handler = slog.NewJSONHandler(os.Stdout, options)
	}

	return slog.New(handler)
}

func ResolveLevel(logLevelStr string) slog.Level {
	switch strings.ToLower(logLevelStr) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func ResolveType(logTypeStr string) Type {
	switch strings.ToLower(logTypeStr) {
	case string(TypeText):
		return TypeText
	case string(TypeJSON):
		return TypeJSON
	default:
		return TypeJSON
	}
}
