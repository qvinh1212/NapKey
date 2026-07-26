// Package logger is a small leveled logger. It exists so napkey-core does not
// pull a logging dependency for what amounts to four wrappers around log.Printf,
// and so every line lands on stderr in one predictable shape.
package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// Level orders the severities. Anything below the configured level is dropped.
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var (
	current atomic.Int32
	out     atomic.Pointer[log.Logger]
)

func init() {
	current.Store(int32(LevelInfo))
	out.Store(log.New(os.Stderr, "", log.LstdFlags|log.LUTC))
}

// ParseLevel maps a config string onto a Level, defaulting to info for anything
// unrecognized rather than failing startup over a typo in an env var.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// SetLevel raises or lowers the threshold at runtime.
func SetLevel(l Level) { current.Store(int32(l)) }

// SetOutput redirects output, used by tests to capture lines.
func SetOutput(w io.Writer) {
	out.Store(log.New(w, "", log.LstdFlags|log.LUTC))
}

func emit(l Level, tag, format string, args ...any) {
	if Level(current.Load()) > l {
		return
	}
	out.Load().Output(3, tag+" "+fmt.Sprintf(format, args...))
}

// Debugf logs at debug level.
func Debugf(format string, args ...any) { emit(LevelDebug, "[DEBUG]", format, args...) }

// Infof logs at info level.
func Infof(format string, args ...any) { emit(LevelInfo, "[INFO] ", format, args...) }

// Warnf logs at warn level.
func Warnf(format string, args ...any) { emit(LevelWarn, "[WARN] ", format, args...) }

// Errorf logs at error level.
func Errorf(format string, args ...any) { emit(LevelError, "[ERROR]", format, args...) }
