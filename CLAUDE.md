# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`codemypaper` is a Go terminal agent that takes an arXiv ML paper, extracts its **core method**, and generates a **runnable reference implementation** of it — self-correcting by writing code, running a toy smoke-test, reading the error, and retrying until it executes. Output target is Python/PyTorch.

The honest success bar (verbatim, also goes in the README): *"Generated code runs on toy input and implements the named method"* — **not** "reproduces the paper's reported numbers."

## Project status & how to work in it

This is a **learning project** built incrementally to teach Go (interfaces, goroutines, `net/http`, `os/exec`, `context`). The canonical sources are, in order:
- `PROJECT_SPECIFICATION.md` — the contract (FRs, CLI flags, exit codes, acceptance checklist §9).
- `DESIGN.md` — architecture, interface signatures, the normative agent loop, design decisions D1–D10.
- `BUILD_PLAN.md` — the step-by-step build order with starter code, milestones M1–M4.

**Current state:** **M1–M3 are implemented.** Done: the Cobra CLI with backend selection (`cmd/codemypaper`), both chat backends (`internal/llm` — Ollama + Gemini), the tool registry + `write_file`/`read_file`/`run_command` and the act→observe→recover loop (`internal/tools`, `internal/agent`), and arXiv ingestion (`internal/arxiv` — `ParseID`, `Fetch` with ar5iv→e-print fallback parsed via **goquery**, `Paper.PromptText`) feeding the real paper-driven prompt (`internal/agent/prompt.go`). **Not yet built** (still the planned target): the optional vision pre-pass (`internal/vision`, `VLMClient`, figure parsing — M3.5) and the M4 polish — the `finish` tool struct, the `Outcome`/`StopReason`/`--wall-budget` loop refactor, `RUN_SUMMARY.md`/`transcript.jsonl`, guardrail/fetch tests, Makefile, README. When asked to add a feature, follow the build order and the signatures in `DESIGN.md` rather than inventing a new shape.

Note: the loop still uses the M2/M3 signature `Agent.Run(ctx, systemPrompt, task string) error`; the `Run(ctx, *arxiv.Paper, outDir) (Outcome, error)` shape in `DESIGN.md` §3.5 is the M4 target, not current.

Because it's a learning project, the user generally wants to understand and write the Go themselves — prefer explaining the idiom and the why over dumping finished code. The `go-teacher` skill exists for this. Confirm before scaffolding large amounts of code unprompted.

## Commands

The module name is the bare local `codemypaper` (not yet a GitHub path), so all package imports are `codemypaper/internal/...`.

```bash
go run ./cmd/codemypaper run <arxiv-id>   # fetch the paper + run the agent loop (default --model gemini)
go run ./cmd/codemypaper version
go build ./...                            # compile-check everything
go vet ./...                              # lint
gofmt -w .                                # format
go test ./...                             # all tests
go test ./internal/arxiv -run TestParseID # a single package / test
go mod tidy                               # after adding/removing deps
```

Local dev model (M1–M2) runs against Ollama: `ollama serve` then `ollama pull qwen2.5-coder:3b`. Override the host with `OLLAMA_HOST`.

A `Makefile` is planned (`build`/`install`/`test`/`lint`, with `go build -ldflags "-X main.version=..."`) but does not exist yet.

## Architecture

Data flow (target): CLI parses flags and resolves backends → `arxiv` fetches & trims method-focused source text (+ optional figure notes) → `agent` builds a system prompt and runs the loop → generated project + `RUN_SUMMARY.md` land in `out/<arxiv-id>/`.

Two abstractions carry the whole design:
- **`LLMClient`** (`internal/llm/client.go`) — the single seam to any chat backend (`Chat(ctx, []Message) (string, error)` + `Name()`). Selecting a backend is a runtime flag, not a code change. A parallel optional `VLMClient` seam handles figure-description vision.
- **`Tool` + `Registry`** — the agent's hands (`write_file`, `read_file`, `run_command`, `finish`). Adding a capability = register one struct. Tools return a `Result{Output, IsError, ExitCode}`.

### Tool-call protocol (important, non-standard)
Tool-calling is a **custom text protocol**, not provider-native function calling — this keeps `LLMClient` plain text-in/text-out and identical across Ollama/Gemini. The model emits optional prose followed by **exactly one** fenced ` ```json ` block with a `tool` key; the parser takes the **last** such block:
```json
{ "tool": "write_file", "args": { "path": "model.py", "content": "import torch..." } }
```
A malformed reply gets **one** corrective re-prompt (which counts against max-iters). The loop stops on the first of: `finish` called · max-iters · wall-budget · fatal error after retry. See `DESIGN.md` §4–§5 for the normative loop and parser rules.

### Guardrails (process-level, not a real sandbox — run only trusted papers)
`run_command` enforces an allowlist (`python(3)`, `pip(3)`, `ls`, `cat`, `pytest`), a per-command timeout, and an output cap. Both `write_file`/`read_file` and `run_command` are confined to the output dir via a cwd-jail that rejects `..` and absolute paths. The agent writes its own `smoke_test.py`; the harness only runs it.

## Conventions & gotchas

- **Secrets come from env only, never flags**, and are never logged or written to the verbose transcript: `GEMINI_API_KEY`, `GROQ_API_KEY`, `OLLAMA_HOST`.
- **Exit codes are part of the contract:** `0` smoke-test passed · `1` budget exhausted, not green · `2` usage/config error (e.g. missing key) · `3` fatal (fetch failed, backend unreachable).
- arXiv ingestion uses **ar5iv HTML → text**, falling back to the e-print LaTeX tarball; it deliberately avoids PDF parsing. The `arxiv` package must not import `llm` or `vision`.
- Vision is an **ingestion pre-pass, not a multimodal chat loop**: it describes figures into text that augments the prompt, and is fully optional — `vlm == nil` is a no-op and per-figure failures are non-fatal.
- `.gitignore` excludes all `*.md` (including the design docs and this file) and `.venv/` — only Go source is pushed. Generated smoke-tests expect a Python venv with `torch`/`numpy` active in the shell.
