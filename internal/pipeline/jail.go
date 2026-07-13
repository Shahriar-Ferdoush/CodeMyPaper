package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeJoin resolves rel against base and rejects any result that escapes base.
// File paths in a model reply are model-controlled input; this is the jail
// boundary the write step relies on.
// Input:
//   - base: the jail root
//   - rel: a path relative to base, as supplied by the model
//
// Output:
//   - string: the resolved absolute path
//   - error: if rel is empty, absolute, or resolves outside base
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
