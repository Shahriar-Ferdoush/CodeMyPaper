package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeJoin keeps file/command access inside base; rejects ".." and absolute paths.
func safeJoin(base, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths not allowed: %s", rel)
	}
	full, err := filepath.Abs(filepath.Join(base, rel))
	if err != nil {
		return "", err
	}
	rbase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	if full != rbase && !strings.HasPrefix(full, rbase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes project dir: %s", rel)
	}
	return full, nil
}

func capOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…[truncated]"
}
