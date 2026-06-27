package log

import (
	"fmt"
	"io"
	"sync"
)
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type Logger struct {
	mu    sync.Mutex
	w     io.Writer
	level Level
}

func New(w io.Writer, verbose bool) *Logger {
	level := LevelInfo
	if verbose {
		level = LevelDebug
	}
	return &Logger{w: w, level: level}
}

func (l *Logger) logf(level Level, prefix, format string, args ...any) {
	if l == nil || level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, prefix+format+"\n", args...)
}

func (l *Logger) Debugf(format string, args ...any) { l.logf(LevelDebug, "[debug] ", format, args...) }
func (l *Logger) Infof(format string, args ...any)  { l.logf(LevelInfo, "[info]  ", format, args...) }
func (l *Logger) Warnf(format string, args ...any)  { l.logf(LevelWarn, "[warn]  ", format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.logf(LevelError, "[error] ", format, args...) }
