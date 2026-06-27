package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"codemypaper/internal/log"
)

func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}

// WriteFile creates or overwrites a file in the jail base directory.
type WriteFile struct {
	base string
	log  *log.Logger
}

func NewWriteFile(base string, logger *log.Logger) *WriteFile {
	return &WriteFile{base: base, log: logger}
}

func (w *WriteFile) Name() string { return "write_file" }

func (w *WriteFile) Description() string {
	return `write_file: create or overwrite a file in the output directory. args: {"path": string, "content": string}`
}

func (w *WriteFile) Run(_ context.Context, args map[string]any) (Result, error) {
	path, err := argString(args, "path")
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	content, err := argString(args, "content")
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	full, err := safeJoin(w.base, path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Result{Output: fmt.Sprintf("create parent dirs: %v", err), IsError: true}, nil
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return Result{Output: fmt.Sprintf("write file: %v", err), IsError: true}, nil
	}
	w.log.Debugf("write_file %s (%d bytes)", path, len(content))
	return Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(content), path)}, nil
}

// ReadFile reads a file back from the jail base directory.
type ReadFile struct {
	base string
	log  *log.Logger
}

func NewReadFile(base string, logger *log.Logger) *ReadFile {
	return &ReadFile{base: base, log: logger}
}

func (r *ReadFile) Name() string { return "read_file" }

func (r *ReadFile) Description() string {
	return `read_file: read a file from the output directory. args: {"path": string}`
}

func (r *ReadFile) Run(_ context.Context, args map[string]any) (Result, error) {
	path, err := argString(args, "path")
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	full, err := safeJoin(r.base, path)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return Result{Output: fmt.Sprintf("read file: %v", err), IsError: true}, nil
	}
	r.log.Debugf("read_file %s (%d bytes)", path, len(data))
	return Result{Output: capOutput(string(data), defaultMaxOutput)}, nil
}
