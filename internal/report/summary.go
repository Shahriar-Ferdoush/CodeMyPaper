// Package report writes the always-present run artifacts:
// RUN_SUMMARY.md (PROJECT_SPECIFICATION §7).
package report

import (
	"fmt"
	"os"
	"strings"
)

// SuccessBar is the §1 honest success bar, reproduced verbatim in every summary
// (and the README). Do not paraphrase.
const SuccessBar = `"Generated code runs on toy input and implements the named method" — not "reproduces the paper's reported numbers."`

// Summary is the data RUN_SUMMARY.md records about a run. Every field is known
// by the time the loop returns; the summary is written on success and failure
// alike.
type Summary struct {
	ID, Title        string
	ChatBackend      string
	VisionBackend    string
	Method           string
	Entrypoint       string
	Iterations       int
	ExitCode         int
	StopReason       string
	FiguresDescribed int
	Truncated        bool
	Caveats          []string
}

// WriteSummary writes RUN_SUMMARY.md at path. It is always called after the
// loop, so the file is present whether the run went green or not.
func WriteSummary(path string, s Summary) error {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("# Run Summary\n\n")
	w("- **arXiv ID:** %s\n", dash(s.ID))
	w("- **Title:** %s\n", dash(s.Title))
	w("- **Chat backend:** %s\n", dash(s.ChatBackend))
	w("- **Vision backend:** %s\n", dash(s.VisionBackend))
	w("- **Method:** %s\n", dash(s.Method))
	w("- **Entrypoint:** %s\n", dash(s.Entrypoint))
	w("- **Iterations:** %d\n", s.Iterations)
	w("- **Stop reason:** %s\n", dash(s.StopReason))
	w("- **Exit code:** %d\n", s.ExitCode)
	w("- **Figures described:** %d\n", s.FiguresDescribed)
	w("- **Paper truncated:** %t\n", s.Truncated)

	if len(s.Caveats) > 0 {
		w("\n## Caveats\n\n")
		for _, c := range s.Caveats {
			w("- %s\n", c)
		}
	}

	w("\n## Success bar\n\n")
	w("%s\n", SuccessBar)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
