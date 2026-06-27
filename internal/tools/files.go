package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type WriteFile struct{ BaseDir string }

func (w WriteFile) Name() string { return "write_file" }
func (w WriteFile) Description() string {
	return "write_file(path, content): create/overwrite a file in the project dir"
}

func (w WriteFile) Run(_ context.Context, args map[string]any) (Result, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	full, err := safeJoin(w.BaseDir, path)
	if err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	return Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(content), path)}, nil
}

type ReadFile struct{ BaseDir string }

func (r ReadFile) Name() string        { return "read_file" }
func (r ReadFile) Description() string { return "read_file(path): read a file in the project dir" }

func (r ReadFile) Run(_ context.Context, args map[string]any) (Result, error) {
	path, _ := args["path"].(string)
	full, err := safeJoin(r.BaseDir, path)
	if err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	return Result{Output: capOutput(string(b), maxOutputBytes)}, nil
}
