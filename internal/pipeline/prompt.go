package pipeline

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"codemypaper/internal/arxiv"
)

// Prompt text lives in prompt.md, embedded into the binary at build time.
// Only the dynamic slots (truncation notice, paper text) are template actions.
// A missing file fails `go build`; a bad template panics at start via Must.
//
//go:embed prompt.md
var systemPromptText string

var systemPromptTmpl = template.Must(template.New("system").Parse(systemPromptText))

// promptData fills the template's dynamic slots.
type promptData struct {
	Truncated bool
	PaperText string
}

// Renders the generate call's system prompt: role, success bar, the reply
// format, and the paper's method text.
// Input:
//   - paper: the paper handed to the model
//   - maxChars: char budget for the paper text
//
// Output:
//   - string: the rendered system prompt
func buildSystemPrompt(paper *arxiv.Paper, maxChars int) string {
	paperText, truncated := paper.PromptText(maxChars)
	var b strings.Builder
	if err := systemPromptTmpl.Execute(&b, promptData{Truncated: truncated, PaperText: paperText}); err != nil {
		// Must only validates template syntax, not field names.
		// A renamed promptData field surfaces here as a panic on the next run.
		panic(fmt.Sprintf("pipeline: render system prompt: %v", err))
	}
	return b.String()
}

// The user message that triggers the generate call.
// Input:
//   - paper: the paper handed to the model
//
// Output:
//   - string: the rendered task message
func generateTask(paper *arxiv.Paper) string {
	title := paper.Title
	if title == "" {
		title = "title unavailable"
	}
	return fmt.Sprintf("Implement the core method of arXiv:%s (%s). "+
		"Reply with the METHOD line and the required file blocks.", paper.ID, title)
}

// The user message that triggers the single repair call. Continues the
// generate conversation, so the model already has the paper text and its own
// files in context; only the failure is new information.
// Input:
//   - res: the failing smoke-test result
//
// Output:
//   - string: the rendered task message
func debugTask(res testResult) string {
	return fmt.Sprintf("Running `python3 %s` failed (exit %d):\n\n%s\n\n"+
		"Fix the code. Reply in the same format with only the file blocks you are changing "+
		"(same file names); do not introduce new files. No prose outside the blocks.",
		smokeTestFile, res.ExitCode, res.Output)
}

// The single re-prompt sent after a malformed reply.
// Input:
//   - reason: the parse/validation error the model should correct
//
// Output:
//   - string: the rendered corrective message
func correctiveTask(reason error) string {
	return fmt.Sprintf("Your previous reply could not be used (%v). "+
		"Reply again in exactly the required format: an optional `METHOD:` line, then one "+
		"`=== FILE: <name> ===` ... `=== END FILE ===` block per file, each marker alone on "+
		"its own line, and nothing else.", reason)
}
