package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultMaxOutput = 8 * 1024

func safeJoin(base, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed: %q", rel)
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve base dir: %w", err)
	}
	
	joined := filepath.Join(absBase, rel)
	relToBase, err := filepath.Rel(absBase, joined)
	if err != nil {
		return "", fmt.Errorf("path %q escapes base: %w", rel, err)
	}
	if relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the output directory", rel)
	}
	return joined, nil
}

func capOutput(s string, max int) string {
	if max <= 0 {
		max = defaultMaxOutput
	}
	if len(s) <= max {
		return s
	}
	omitted := len(s) - max
	head := strings.ToValidUTF8(s[:max], "")
	return head + fmt.Sprintf("\n... [output truncated: %d bytes omitted]", omitted)
}
