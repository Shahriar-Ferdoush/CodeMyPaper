package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codemypaper/internal/arxiv"
	"codemypaper/internal/llm"
	"codemypaper/internal/log"
)

// Stop reasons for Outcome.StopReason.
const (
	StopPassed          = "passed"           // smoke test exited 0
	StopRepairExhausted = "repair_exhausted" // both test runs failed
	StopMalformed       = "malformed_reply"  // reply unusable after the one corrective re-prompt
	StopFatal           = "fatal_error"      // chat backend failure
)

// requiredFiles is what the generate call must produce.
var requiredFiles = []string{"model.py", smokeTestFile, "main.py"}

// Config holds the pipeline's tunables.
type Config struct {
	OutDir          string
	TestTimeout     time.Duration
	MaxContextChars int
}

// Outcome is the result of a full pipeline run.
type Outcome struct {
	Success    bool
	Method     string // from the reply's METHOD line
	StopReason string
	FirstFail  string // captured failing test output(s), for ERROR_LOG.md
	SecondFail string
}

// errMalformed marks a reply that stayed unusable after the corrective re-prompt.
var errMalformed = errors.New("malformed model reply")

// Drives the fixed two-call pipeline: generate → write files → smoke-test →
// on failure one debug call → re-test. Control flow lives here, not in the model;
// the model is called at most twice (plus at most one corrective re-prompt per call).
// Input:
//   - ctx: context.Context for cancellation and deadlines
//   - client: the LLM backend to drive
//   - paper: the ingested paper handed to the model
//   - cfg: output dir, smoke-test timeout, context budget
//   - logger: run logger
//
// Output:
//   - Outcome: the run's ending, encoded even on failure
//   - error: non-nil only for fatal chat-backend failures; every other ending is in Outcome
//
// REPORT.md is written on every ending.
func Run(ctx context.Context, client llm.LLMClient, paper *arxiv.Paper, cfg Config, logger *log.Logger) (Outcome, error) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: buildSystemPrompt(paper, cfg.MaxContextChars)},
		{Role: llm.RoleUser, Content: generateTask(paper)},
	}
	rep := runReport{Paper: paper, Backend: client.Name()}
	reserved := reservedNames(paper)

	// Remove a stale error log so a green rerun can't report two contradicting endings.
	if err := os.Remove(filepath.Join(cfg.OutDir, errorLogFile)); err == nil {
		logger.Debugf("removed stale %s", errorLogFile)
	}

	finish := func(o Outcome) (Outcome, error) {
		rep.Outcome = o
		writeReport(cfg.OutDir, rep, logger)
		return o, nil
	}

	// LLM call #1: generate model.py, smoke_test.py, main.py.
	logger.Infof("LLM call 1/2: generate")
	reply, raw, err := chatForFiles(ctx, client, &messages, logger, func(r Reply) error {
		return validateGenerate(r, cfg.OutDir, reserved)
	})
	if err != nil {
		if errors.Is(err, errMalformed) {
			logger.Errorf("generate reply unusable: %v", err)
			writeErrorLog(cfg.OutDir, Outcome{StopReason: StopMalformed}, raw, nil, logger)
			return finish(Outcome{StopReason: StopMalformed})
		}
		rep.Outcome = Outcome{StopReason: StopFatal}
		writeReport(cfg.OutDir, rep, logger)
		return Outcome{StopReason: StopFatal}, err
	}
	rep.Outcome.Method = reply.Method
	if err := writeFiles(cfg.OutDir, reply.Files, &rep, logger); err != nil {
		rep.Outcome.StopReason = StopFatal
		writeReport(cfg.OutDir, rep, logger)
		return Outcome{StopReason: StopFatal, Method: reply.Method}, err
	}

	// First smoke-test run.
	first := runSmokeTest(ctx, cfg.OutDir, cfg.TestTimeout, logger)
	rep.Verdicts = append(rep.Verdicts, first)
	if first.passed() {
		logger.Infof("smoke test passed on the first run")
		return finish(Outcome{Success: true, Method: reply.Method, StopReason: StopPassed})
	}
	logger.Infof("smoke test failed (exit %d) — one repair attempt", first.ExitCode)

	// LLM call #2: the single repair attempt (D9 policy).
	// Conversation continues, so the model still has the paper and its files in context.
	written := make(map[string]bool, len(rep.Files))
	for _, n := range rep.Files {
		written[n] = true
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: debugTask(first)})
	logger.Infof("LLM call 2/2: debug")
	fixed, raw, err := chatForFiles(ctx, client, &messages, logger, func(r Reply) error {
		return validateDebug(r, cfg.OutDir, reserved, written)
	})
	if err != nil {
		if errors.Is(err, errMalformed) {
			logger.Errorf("debug reply unusable: %v", err)
			o := Outcome{Method: reply.Method, StopReason: StopMalformed, FirstFail: first.Output}
			writeErrorLog(cfg.OutDir, o, raw, nil, logger)
			return finish(o)
		}
		rep.Outcome.StopReason = StopFatal
		writeReport(cfg.OutDir, rep, logger)
		return Outcome{StopReason: StopFatal, Method: reply.Method}, err
	}
	rewritten := fileNames(fixed.Files)
	if err := writeFiles(cfg.OutDir, fixed.Files, nil, logger); err != nil {
		rep.Outcome.StopReason = StopFatal
		writeReport(cfg.OutDir, rep, logger)
		return Outcome{StopReason: StopFatal, Method: reply.Method}, err
	}

	// Second (final) smoke-test run.
	second := runSmokeTest(ctx, cfg.OutDir, cfg.TestTimeout, logger)
	rep.Verdicts = append(rep.Verdicts, second)
	if second.passed() {
		logger.Infof("smoke test passed after the repair")
		return finish(Outcome{Success: true, Method: reply.Method, StopReason: StopPassed})
	}

	logger.Infof("smoke test failed again (exit %d) — stopping", second.ExitCode)
	o := Outcome{
		Method:     reply.Method,
		StopReason: StopRepairExhausted,
		FirstFail:  first.Output,
		SecondFail: second.Output,
	}
	writeErrorLog(cfg.OutDir, o, "", rewritten, logger)
	return finish(o)
}

// Performs one logical LLM call: chat, parse, validate, with at most one
// corrective re-prompt on a malformed reply. Appends every message it sends
// or receives to *messages so a later call continues the conversation.
// Input:
//   - ctx: context.Context for cancellation and deadlines
//   - client: the LLM backend to call
//   - messages: the conversation so far; mutated in place with every sent/received turn
//   - logger: run logger
//   - validate: checks the parsed reply for this call (generate vs debug rules)
//
// Output:
//   - Reply: the validated reply
//   - string: the last raw reply text, for logging on errMalformed
//   - error: wrapped errMalformed if both attempts failed, or a chat transport error
func chatForFiles(ctx context.Context, client llm.LLMClient, messages *[]llm.Message,
	logger *log.Logger, validate func(Reply) error) (Reply, string, error) {

	var raw string
	for attempt := 1; attempt <= 2; attempt++ {
		var err error
		raw, err = client.Chat(ctx, *messages)
		if err != nil {
			return Reply{}, "", fmt.Errorf("chat: %w", err)
		}
		*messages = append(*messages, llm.Message{Role: llm.RoleAssistant, Content: raw})

		reply, perr := parseReply(raw)
		if perr == nil {
			perr = validate(reply)
		}
		if perr == nil {
			return reply, raw, nil
		}
		logger.Debugf("attempt %d malformed: %v", attempt, perr)
		if attempt == 1 {
			*messages = append(*messages, llm.Message{Role: llm.RoleUser, Content: correctiveTask(perr)})
			continue
		}
		return Reply{}, raw, fmt.Errorf("%w: %v", errMalformed, perr)
	}
	panic("unreachable")
}

// Checks the generate reply: all required files present, every name jailed
// and non-reserved. A violation counts as malformed → re-prompt.
// Input:
//   - r: the parsed reply
//   - outDir: the run's output directory, for the path jail
//   - reserved: pipeline-owned names the reply may not write
//
// Output:
//   - error: the first validation failure found, or nil
func validateGenerate(r Reply, outDir string, reserved map[string]bool) error {
	for _, want := range requiredFiles {
		if !hasFile(r.Files, want) {
			return fmt.Errorf("required file %q is missing", want)
		}
	}
	return validateNames(r.Files, outDir, reserved)
}

// Checks the repair reply: only files written by the generate call may be rewritten.
// Input:
//   - r: the parsed reply
//   - outDir: the run's output directory, for the path jail
//   - reserved: pipeline-owned names the reply may not write
//   - written: file names the generate call already wrote
//
// Output:
//   - error: the first validation failure found, or nil
func validateDebug(r Reply, outDir string, reserved, written map[string]bool) error {
	for _, f := range r.Files {
		if !written[filepath.Clean(f.Name)] {
			return fmt.Errorf("file %q was not part of the generated project; rewrite existing files only", f.Name)
		}
	}
	return validateNames(r.Files, outDir, reserved)
}

// Applies the path jail and the reserved-name check to every file. Reserved
// names protect pipeline-owned artifacts (run.log, REPORT.md, …) from being
// clobbered by model content sharing the same directory.
// Input:
//   - files: the files to check
//   - outDir: the run's output directory, for the path jail
//   - reserved: pipeline-owned names, keyed lowercase
//
// Output:
//   - error: a jail error, a reserved-name error, or nil
//
// The reserved-name comparison is case-insensitive: macOS's default filesystem
// is case-insensitive, so "Run.Log" would otherwise resolve to the same file as
// the live run.log.
func validateNames(files []File, outDir string, reserved map[string]bool) error {
	for _, f := range files {
		if _, err := safeJoin(outDir, f.Name); err != nil {
			return err
		}
		if reserved[strings.ToLower(filepath.Clean(f.Name))] {
			return fmt.Errorf("file name %q is reserved for the tool's own output", f.Name)
		}
	}
	return nil
}

// Returns the set of pipeline-owned artifact names in outDir, keyed lowercase
// for the case-insensitive check in validateNames.
// Input:
//   - paper: the current run's paper, for its persisted raw-source name
//
// Output:
//   - map[string]bool: the reserved name set, keyed lowercase
func reservedNames(paper *arxiv.Paper) map[string]bool {
	r := map[string]bool{
		"run.log":                     true,
		"paper.meta.json":             true,
		strings.ToLower(reportFile):   true,
		strings.ToLower(errorLogFile): true,
	}
	if paper.RawName != "" {
		r[strings.ToLower(paper.RawName)] = true
	}
	return r
}

// Writes each parsed file under outDir (names were validated; the jail here
// is belt-and-braces). Write failures are fatal — they are our environment's
// fault, not the model's, so no re-prompt.
// Input:
//   - outDir: the run's output directory
//   - files: the files to write
//   - rep: if non-nil, the written names are recorded on it
//   - logger: run logger
//
// Output:
//   - error: on a jail, mkdir, or write failure
func writeFiles(outDir string, files []File, rep *runReport, logger *log.Logger) error {
	for _, f := range files {
		path, err := safeJoin(outDir, f.Name)
		if err != nil {
			return fmt.Errorf("write %q: %w", f.Name, err)
		}
		if dir := filepath.Dir(path); dir != outDir {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("write %q: %w", f.Name, err)
			}
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("write %q: %w", f.Name, err)
		}
		logger.Debugf("wrote %s (%d bytes)", f.Name, len(f.Content))
		if rep != nil {
			rep.Files = append(rep.Files, filepath.Clean(f.Name))
		}
	}
	return nil
}

// Reports whether files contains one named name (path-cleaned).
func hasFile(files []File, name string) bool {
	for _, f := range files {
		if filepath.Clean(f.Name) == name {
			return true
		}
	}
	return false
}

// Returns each file's path-cleaned name, in order.
func fileNames(files []File) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Clean(f.Name)
	}
	return names
}
