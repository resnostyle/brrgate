package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// Level controls which messages are emitted.
type Level int32

const (
	LevelDebug Level = 10
	LevelInfo  Level = 20
	LevelWarn  Level = 30
	LevelError Level = 40
)

// Logger is a tiny leveled logger with key=value pairs (no external deps).
type Logger struct {
	base   *log.Logger
	thresh int32
}

// New creates a logger writing to w (stdout if nil) with the minimum level.
func New(w io.Writer, min Level) *Logger {
	if w == nil {
		w = os.Stdout
	}
	return &Logger{
		base:   log.New(w, "", log.LstdFlags|log.LUTC),
		thresh: int32(min),
	}
}

// ParseLevel maps common strings to levels.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info", "":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l *Logger) allow(msg Level) bool {
	return int32(msg) >= atomic.LoadInt32(&l.thresh)
}

// Allows reports whether messages at the provided level would print.
func (l *Logger) Allows(msg Level) bool {
	return l.allow(msg)
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, pairs ...any) {
	l.emit(LevelDebug, "DEBUG", msg, pairs)
}

// Info logs an informational message.
func (l *Logger) Info(msg string, pairs ...any) {
	l.emit(LevelInfo, "INFO", msg, pairs)
}

// Warn logs a warning.
func (l *Logger) Warn(msg string, pairs ...any) {
	l.emit(LevelWarn, "WARN", msg, pairs)
}

// Error logs an error.
func (l *Logger) Error(msg string, pairs ...any) {
	l.emit(LevelError, "ERROR", msg, pairs)
}

func (l *Logger) emit(msgLevel Level, tag, msg string, pairs []any) {
	if !l.allow(msgLevel) {
		return
	}
	b := strings.Builder{}
	b.WriteString(tag)
	b.WriteString(" ")
	b.WriteString(msg)
	if len(pairs) == 0 {
		l.base.Println(b.String())
		return
	}
	if len(pairs)%2 != 0 {
		pairs = append(pairs, "kv", "malformed")
	}
	for i := 0; i < len(pairs); i += 2 {
		key, _ := pairs[i].(string)
		b.WriteString(" ")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(fmt.Sprint(pairs[i+1]))
	}
	l.base.Println(b.String())
}
