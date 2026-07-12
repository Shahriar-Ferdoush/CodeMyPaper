package agent

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"codemypaper/internal/arxiv"
	"codemypaper/internal/tools"
)

// The prompt text lives in prompt.md (compiled into the binary at build time);
// only the dynamic slots — tool catalog, truncation notice, paper text — are
// template actions. A missing file fails `go build`; a template syntax error
// fails at program start via Must (package init — so it crashes every
// subcommand, not just `run`).
//
//go:embed prompt.md
var systemPromptText string

var systemPromptTmpl = template.Must(template.New("system").Parse(systemPromptText))

// promptData fills the template's dynamic slots.
type promptData struct {
	ToolDescriptions []string
	Truncated        bool
	PaperText        string
}

// BuildSystemPrompt assembles the system prompt: role, success bar, tool catalog and
// protocol, hard rules, and the paper's method text.
// Input:
//   - reg: the tool registry, used to render the tool catalog
//   - paper: the fetched paper
//   - maxChars: the char budget applied to the paper text (0 = unlimited)
//
// Output:
//   - string: the rendered system prompt
func BuildSystemPrompt(reg *tools.Registry, paper *arxiv.Paper, maxChars int) string {
	paperText, truncated := paper.PromptText(maxChars)

	data := promptData{Truncated: truncated, PaperText: paperText}
	for _, t := range reg.Tools() {
		data.ToolDescriptions = append(data.ToolDescriptions, t.Description())
	}

	var b strings.Builder
	if err := systemPromptTmpl.Execute(&b, data); err != nil {
		// Must validates template syntax only, not field names: renaming a
		// promptData field without updating prompt.md (or vice versa) lands
		// here on the next run. Programmer error → panic, same class as
		// Registry's duplicate check; a golden-output test pins this at FEAT-405.
		panic(fmt.Sprintf("agent: render system prompt: %v", err))
	}
	return b.String()
}

// FirstUserMessage is the task that kicks off the loop.
func FirstUserMessage(paper *arxiv.Paper) string {
	title := paper.Title
	if title == "" {
		title = "title unavailable"
	}
	return fmt.Sprintf("Implement the core method of arXiv:%s (%s). Produce `model.py` and "+
		"`smoke_test.py`. Iterate until the smoke-test passes, then call finish.", paper.ID, title)
}
