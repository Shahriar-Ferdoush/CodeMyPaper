package log

import (
	"fmt"
	"io"
	"sync"
)

// Level is a logger's minimum severity to emit.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Logger writes leveled, prefixed lines to w. Safe for concurrent use.
type Logger struct {
	mu    sync.Mutex
	w     io.Writer
	level Level
}

// New creates a Logger writing to w. verbose sets the minimum level to LevelDebug;
// otherwise it's LevelInfo.
func New(w io.Writer, verbose bool) *Logger {
	level := LevelInfo
	if verbose {
		level = LevelDebug
	}
	return &Logger{w: w, level: level}
}

// logf writes a prefixed line if level is at or above the logger's minimum level.
// A nil Logger or nil writer discards silently.
func (l *Logger) logf(level Level, prefix, format string, args ...any) {
	if l == nil || l.w == nil || level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, prefix+format+"\n", args...)
}

// Debugf logs at LevelDebug.
func (l *Logger) Debugf(format string, args ...any) { l.logf(LevelDebug, "[debug] ", format, args...) }

// Infof logs at LevelInfo.
func (l *Logger) Infof(format string, args ...any) { l.logf(LevelInfo, "[info]  ", format, args...) }

// Warnf logs at LevelWarn.
func (l *Logger) Warnf(format string, args ...any) { l.logf(LevelWarn, "[warn]  ", format, args...) }

// Errorf logs at LevelError.
func (l *Logger) Errorf(format string, args ...any) { l.logf(LevelError, "[error] ", format, args...) }
