package agent

import (
	"fmt"
	"strings"

	"codemypaper/internal/arxiv"
	"codemypaper/internal/tools"
)

// BuildSystemPrompt assembles the system prompt in the DESIGN §6 order:
//  1. role,
//  2. the honest success bar (verbatim from PROJECT_SPECIFICATION §1),
//  3. tool catalog + the single-block JSON protocol with one example,
//  4. hard rules,
//  5. the paper text (with a truncation notice when p.Truncated).
func BuildSystemPrompt(reg *tools.Registry, p *arxiv.Paper) (prompt string, truncated bool) {
	var b strings.Builder

	// (1) Role.
	b.WriteString("You are codemypaper, an expert ML engineer. Your job is to read the ")
	b.WriteString("text of an arXiv paper and implement its CORE METHOD as a minimal, ")
	b.WriteString("self-contained Python/PyTorch program. Implement only the method itself ")
	b.WriteString("(the model/algorithm/objective the paper proposes) — not its full training ")
	b.WriteString("pipeline, datasets, or experiments.\n\n")

	// (2) The honest success bar — VERBATIM from PROJECT_SPECIFICATION §1.
	b.WriteString("Your goal — the honest success bar:\n")
	b.WriteString("\"Generated code runs on toy input and implements the named method\" — ")
	b.WriteString("not \"reproduces the paper's reported numbers.\"\n\n")

	// (3) Tool catalog + the single-block JSON protocol with one example.
	b.WriteString("AVAILABLE TOOLS:\n")
	b.WriteString(reg.Descriptions())
	// finish is handled by the loop, not the registry, so list it here so the
	// advertised catalog matches the protocol section below.
	b.WriteString("- finish: signal completion AFTER the smoke-test passes (args: summary, method, entrypoint)\n")
	b.WriteString("\n")
	b.WriteString("TOOL-CALL PROTOCOL:\n")
	b.WriteString("To act, reply with optional prose followed by EXACTLY ONE fenced ```json ")
	b.WriteString("block containing an object with a \"tool\" key and an \"args\" object. ")
	b.WriteString("If you emit more than one block, only the LAST one is executed. ")
	b.WriteString("Example:\n\n")
	b.WriteString("```json\n")
	b.WriteString(`{"tool":"write_file","args":{"path":"model.py","content":"import torch\n..."}}`)
	b.WriteString("\n```\n\n")
	b.WriteString("When (and only when) the smoke-test passes, signal completion with:\n\n")
	b.WriteString("```json\n")
	b.WriteString(`{"tool":"finish","args":{"summary":"...","method":"...","entrypoint":"smoke_test.py"}}`)
	b.WriteString("\n```\n\n")

	// (4) Hard rules.
	b.WriteString("HARD RULES:\n")
	b.WriteString("- Keep dependencies minimal (prefer just torch/numpy); do not invent imports.\n")
	b.WriteString("- Write the method implementation to `model.py`.\n")
	b.WriteString("- Write `smoke_test.py` that exercises the method on tiny synthetic tensors ")
	b.WriteString("and asserts the outputs are finite and have the correct shape.\n")
	b.WriteString("- RUN the smoke-test via run_command (e.g. `python3 smoke_test.py`).\n")
	b.WriteString("- On failure, read the error output, fix the code, and run it again.\n")
	b.WriteString("- Call `finish` ONLY after the smoke-test exits 0.\n\n")

	// (5) The paper text, clearly delimited.
	paperText, truncated := p.PromptText()
	if truncated {
		b.WriteString("NOTE: the paper text below was trimmed to fit the context budget.\n")
	}
	b.WriteString("PAPER START\n")
	b.WriteString(paperText)
	b.WriteString("\nPAPER END\n")

	return b.String(), truncated
}

// BuildTask returns the first user message describing the concrete task.
func BuildTask(p *arxiv.Paper) string {
	return fmt.Sprintf(
		"Implement the core method of arXiv:%s (%s). Produce model.py and smoke_test.py. "+
			"Iterate until the smoke-test passes, then finish.",
		p.ID, strings.TrimSpace(p.Title))
}
