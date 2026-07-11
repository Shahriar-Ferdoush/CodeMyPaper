package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultMaxOutput is the byte cap applied to tool output before it's fed back to the model.
const defaultMaxOutput = 8 * 1024

// safeJoin resolves rel against base and rejects any result that escapes base.
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
	// Reject any ".." segment here — this is the jail boundary the tools rely on.
	if relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the output directory", rel)
	}
	return joined, nil
}

// capOutput truncates s to max bytes, appending a notice of how much was omitted.
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
