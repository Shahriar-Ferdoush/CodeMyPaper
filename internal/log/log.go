// Package log is the run's leveled logger: console for live triage, an
// attachable per-run file as the forensic record.
package log

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level is a logger's minimum severity to emit.
type Level int

// Severity levels, lowest to highest.
const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Logger writes leveled, prefixed, timestamped lines to a console writer and,
// once AttachFile is called, to a per-run log file. The console respects the
// verbose level; the file always records everything down to LevelDebug — the
// terminal is for live triage, the file is the forensic record.
// Safe for concurrent use.
type Logger struct {
	mu    sync.Mutex
	w     io.Writer
	level Level
	file  *os.File
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

// AttachFile opens path (truncating any previous run's log) as a second sink that
// records all levels. May be called at most once; a second call errors.
func (l *Logger) AttachFile(path string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return fmt.Errorf("log file already attached")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304 -- path is the run's own log file under the out dir, chosen by our code
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	l.file = f
	return nil
}

// Close closes the attached log file, if any.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// Writes a prefixed, timestamped line to each sink whose threshold admits
// level: the console at the logger's configured level, the file at LevelDebug.
// A nil Logger discards silently.
func (l *Logger) logf(level Level, prefix, format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil && l.file == nil {
		return
	}
	// format must be a literal string, never dynamic text (e.g. an LLM reply).
	// Dynamic text can carry stray %-verbs and corrupt the line — pass it as an arg instead.
	line := fmt.Sprintf(time.Now().Format(time.RFC3339)+" "+prefix+format+"\n", args...)
	// Logging is best-effort by design: a sink write failure must not fail the
	// run it describes, and there is no saner sink to report it to.
	if l.w != nil && level >= l.level {
		_, _ = io.WriteString(l.w, line)
	}
	if l.file != nil {
		_, _ = io.WriteString(l.file, line)
	}
}

// Debugf logs at LevelDebug.
func (l *Logger) Debugf(format string, args ...any) { l.logf(LevelDebug, "[debug] ", format, args...) }

// Infof logs at LevelInfo.
func (l *Logger) Infof(format string, args ...any) { l.logf(LevelInfo, "[info]  ", format, args...) }

// Warnf logs at LevelWarn.
func (l *Logger) Warnf(format string, args ...any) { l.logf(LevelWarn, "[warn]  ", format, args...) }

// Errorf logs at LevelError.
func (l *Logger) Errorf(format string, args ...any) { l.logf(LevelError, "[error] ", format, args...) }
