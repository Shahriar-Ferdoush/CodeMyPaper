package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codemypaper/internal/arxiv"
	"codemypaper/internal/log"
)

const (
	reportFile   = "REPORT.md"
	errorLogFile = "ERROR_LOG.md"
)

// successBar is the project's honest success criterion, stated verbatim on
// every report.
const successBar = `Success bar: the generated code runs on toy input and implements the named method — it does not reproduce the paper's reported numbers.`

// runReport accumulates what REPORT.md needs as the pipeline progresses.
type runReport struct {
	Paper    *arxiv.Paper
	Backend  string
	Files    []string // written by the generate call
	Verdicts []testResult
	Outcome  Outcome
}

// writeReport writes REPORT.md into outDir. It runs on every ending, success
// and failure alike; a write failure is logged, never fatal — the report must
// not be able to break the run it describes.
func writeReport(outDir string, r runReport, logger *log.Logger) {
	var b strings.Builder
	b.WriteString("# codemypaper run report\n\n")

	title := r.Paper.Title
	if title == "" {
		title = "title unavailable"
	}
	fmt.Fprintf(&b, "- paper: arXiv:%s — %s (source: %s)\n", r.Paper.ID, title, r.Paper.Source)
	fmt.Fprintf(&b, "- backend: %s\n", r.Backend)
	method := r.Outcome.Method
	if method == "" {
		method = "not reported by the model"
	}
	fmt.Fprintf(&b, "- method: %s\n", method)
	b.WriteString("- entrypoint: main.py\n")
	if len(r.Files) > 0 {
		fmt.Fprintf(&b, "- files: %s\n", strings.Join(r.Files, ", "))
	}
	for i, v := range r.Verdicts {
		verdict := fmt.Sprintf("exit %d", v.ExitCode)
		if v.TimedOut {
			verdict = "timed out"
		}
		fmt.Fprintf(&b, "- smoke test run %d: %s (%s)\n", i+1, verdict, v.Duration)
	}
	fmt.Fprintf(&b, "- result: %s (exit %s)\n", r.Outcome.StopReason, exitCodeFor(r.Outcome.StopReason))
	if r.Outcome.StopReason != StopPassed {
		fmt.Fprintf(&b, "- details: see run.log%s\n", errorLogHint(r.Outcome.StopReason))
	}
	b.WriteString("\n" + successBar + "\n")

	writeArtifact(outDir, reportFile, b.String(), logger)
}

// exitCodeFor maps a stop reason to the process exit code the run ends with.
func exitCodeFor(stopReason string) string {
	switch stopReason {
	case StopPassed:
		return "0"
	case StopRepairExhausted, StopMalformed:
		return "1"
	default:
		return "2 or 3, see run.log" // fatal: main decides between config and backend failure
	}
}

func errorLogHint(stopReason string) string {
	if stopReason == StopRepairExhausted || stopReason == StopMalformed {
		return " and " + errorLogFile
	}
	return ""
}

// writeErrorLog writes ERROR_LOG.md: the resumable record of why the run ended
// without a green smoke-test. rawReply is set for malformed endings; rewritten
// names the files the repair call changed, for repair-exhausted endings.
func writeErrorLog(outDir string, o Outcome, rawReply string, rewritten []string, logger *log.Logger) {
	var b strings.Builder
	b.WriteString("# codemypaper error log\n\n")

	switch o.StopReason {
	case StopRepairExhausted:
		b.WriteString("The smoke test failed, the single repair attempt was used, and the test failed again.\n")
		b.WriteString("\n## First failing smoke-test output\n\n")
		writeFenced(&b, o.FirstFail)
		fmt.Fprintf(&b, "\n## Repair\n\nFiles rewritten by the debug call: %s\n", strings.Join(rewritten, ", "))
		b.WriteString("\n## Second failing smoke-test output\n\n")
		writeFenced(&b, o.SecondFail)
	case StopMalformed:
		b.WriteString("A model reply stayed unusable after the one corrective re-prompt; no runnable state was reached.\n")
		if o.FirstFail != "" {
			// The malformed reply came from the debug call: keep the test failure
			// that triggered it, or this log is not resumable.
			b.WriteString("\n## Failing smoke-test output that triggered the repair\n\n")
			writeFenced(&b, o.FirstFail)
		}
		b.WriteString("\n## Last raw model reply\n\n")
		writeFenced(&b, rawReply)
	}

	writeArtifact(outDir, errorLogFile, b.String(), logger)
}

// writeFenced writes s as a fenced block, using a fence longer than any run of
// backticks in s so model output can't break out of it.
func writeFenced(b *strings.Builder, s string) {
	fence := "```"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	fmt.Fprintf(b, "%s\n%s\n%s\n", fence, strings.TrimRight(s, "\n"), fence)
}

// writeArtifact best-effort writes one pipeline-owned file, logging failures.
func writeArtifact(outDir, name, content string, logger *log.Logger) {
	path := filepath.Join(outDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		logger.Errorf("could not write %s: %v", name, err)
		return
	}
	logger.Infof("wrote %s", name)
}
