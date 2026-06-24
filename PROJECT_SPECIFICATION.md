# codemypaper — Specification

> The **contract** for v1 and the basis for **final verification**: *what* codemypaper must do and *how we
> check it*, not how it's built. Implementation lives in [`DESIGN.md`](./DESIGN.md); origin in
> [`PROJECT-BRIEF.md`](./PROJECT-BRIEF.md). §9 is the definition of done — every box checkable against
> behavior, not code shape.

## 1. Overview

`codemypaper` is a Go terminal agent that takes an arXiv ML paper, extracts its **core method**, and
generates a **runnable reference implementation** of it — self-correcting by writing code, running a toy
smoke-test, reading the error, and retrying until the code executes.

**Honest success bar (verbatim in README):**
> *"Generated code runs on toy input and implements the named method"* — **not** *"reproduces the paper's reported numbers."*

## 2. Scope

**In (v1):** arXiv ID/URL input · ingest method-focused source text · agent loop that writes/reads files and
runs commands, self-correcting on smoke-test failure · generate a small Python/PyTorch project (core method +
toy smoke-test) · swap chat model local (Ollama) ↔ hosted (Gemini) by flag · **optional** vision pre-pass
that describes method-relevant figures into the paper context (absent a VLM, runs text-only, unchanged) ·
emit an output folder + run summary.

**Out (v1):** reproducing training pipelines/datasets/reported numbers · PDF math/figure extraction · TUI ·
real OS sandboxing (process-level guardrails only — §6; run only papers you trust) · non-Python output.

## 3. Functional requirements

| ID | Requirement |
|---|---|
| FR1 | Accept an arXiv id (`2401.01234`), a full `abs/`/`pdf/` URL, with/without version suffix, and resolve it. |
| FR2 | Produce method-focused text trimmed to a char budget; flag when truncated. |
| FR3 | Drive an act→observe→recover loop where the model can **write/read files** and **run commands**, and signals completion explicitly. |
| FR4 | Generate the method implementation + a toy **smoke-test** exercising it on tiny synthetic input. |
| FR5 | On smoke-test failure, feed the error back and retry, bounded by max-iters and a wall-clock budget. |
| FR6 | Select the chat backend (`gemini`/`ollama`) at runtime, no code changes. |
| FR7 | **Optionally** run a vision pre-pass over figures (`gemini`/`groq`); degrade to text-only when no backend/key or a call fails. |
| FR8 | Write an output folder + an always-present run summary (method, entrypoint, iterations, stop reason). |
| FR9 | Report success/failure via exit codes (§4). |

## 4. CLI contract

```
codemypaper run <arxiv-id-or-url> [flags]
codemypaper version
```

| Flag | Default | Meaning |
|---|---|---|
| `--model` | `gemini` | Chat backend: `gemini` \| `ollama`. |
| `--gemini-model` | `gemini-2.5-flash` | Hosted chat model id. |
| `--ollama-model` | `qwen2.5-coder:3b` | Local chat model id. |
| `--vision` | `auto` | Vision backend: `auto` \| `gemini` \| `groq` \| `none`. |
| `--gemini-vision-model` | `gemini-2.5-flash` | Vision model id (gemini). |
| `--groq-vision-model` | `llama-3.2-11b-vision` | Vision model id (groq). |
| `--max-figures` | `4` | Max figures sent to the vision backend. |
| `--out` | `./out/<arxiv-id>` | Output directory. |
| `--max-iters` | `6` | Max loop iterations. |
| `--timeout` | `120s` | Per-command timeout. |
| `--wall-budget` | `10m` | Total run budget (`0` = unlimited). |
| `--max-context-chars` | `60000` | Paper-text budget. |
| `--lang` | `python` | Target language (v1: python only). |
| `--verbose` | `false` | Stream the loop. |

**`--vision auto`:** → `gemini` if chat is gemini with `GEMINI_API_KEY`; else `groq` if `GROQ_API_KEY`; else
`none`. The tool always runs, even with no vision key.

**Exit codes:** `0` smoke-test passed · `1` budget exhausted, not green · `2` usage/config error (e.g. missing
key) · `3` fatal (fetch failed, backend unreachable).

## 5. Configuration & secrets
- Secrets from **env only**, never flags: `GEMINI_API_KEY`, `GROQ_API_KEY`, `OLLAMA_HOST` (default
  `http://localhost:11434`).
- Never logged or written to the `--verbose` transcript (redacted).
- No config file in v1; all knobs are §4 flags.

## 6. Non-functional requirements
- NFR1 **cwd-jail** — file/command tools stay within the out dir; `..`/absolute paths rejected.
- NFR2 **allowlist** — only allowlisted command prefixes run; others return an error observation.
- NFR3 **timeouts** — every command bounded by `--timeout`; killed on timeout with a timeout observation.
- NFR4 **output caps** — observations truncated with a marker to protect the context window.
- NFR5 **honesty** — README states the §1 success bar verbatim and that v1 is **not** a sandbox (code runs as
  the invoking user; run only trusted papers).
- NFR6 **robustness** — a malformed model turn triggers one corrective re-prompt, not a crash.
- NFR7 **graceful vision** — vision/figure failures never abort a run; degrade to caption-only.
- NFR8 **portability** — macOS and Linux (Windows best-effort).

## 7. Output artifacts (contract)

`out/<arxiv-id>/`:
- Generated source + `smoke_test.py` (+ a deps file like `requirements.txt` if the agent wrote one).
- `RUN_SUMMARY.md` (**always**): id/title, chat+vision backends, method, entrypoint, iterations, final exit
  code, stop reason, figures-described count, caveats, and the §1 success-bar line.
- `figure_notes.md` (**only** if vision ran): per-figure descriptions with refs/captions.
- `transcript.jsonl` (**only** with `--verbose`): full message/tool/observation log.

## 8. Deployment

Barebones, sized for a ~1-week project. Audience already has Python/torch; the path is **clone +
`make install`**, with `go install` for the no-clone case.

**Prerequisites:** Python 3.10+ on PATH with the generated code's libs (typically `torch`, `numpy`) · Go 1.22+
to build · a model key (`GEMINI_API_KEY`, or `GROQ_API_KEY` for vision) for hosted runs · Ollama running only
for `--model ollama`.

- DR1 — `make install` / `make build` from a fresh clone yields a working binary (Go modules only).
- DR2 — README Quickstart reaches a green smoke-test in ≤5 commands.
- DR3 — Missing required key → exit `2` with a message naming the env var and suggesting `--model ollama`.

## 9. Acceptance criteria (final-verification checklist)

**A. Build & CLI**
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` pass.
- [ ] `--model` switches ollama ↔ gemini with no code change.
- [ ] `codemypaper version` prints a version; `--help` lists every §4 flag.
- [ ] Exit codes match §4.

**B. Behavior (end-to-end)**
- [ ] `run <id> --model ollama` completes a full act→observe→recover loop with no hosted calls.
- [ ] All FR1 id/URL forms resolve; text is method-focused; ar5iv failure falls back to e-print.
- [ ] A malformed model turn → exactly one corrective re-prompt, not a crash (NFR6).
- [ ] On the **pinned reference paper** (`testdata`), `--model gemini` produces `model.py` + `smoke_test.py` where `python smoke_test.py` exits 0 within budget.
- [ ] Self-correction demonstrable: transcript shows ≥1 failed smoke-test then a fix that turns it green.
- [ ] `RUN_SUMMARY.md` always written with method, entrypoint, iterations, stop reason.

**B′. Vision (optional)**
- [ ] `--vision none`/`auto`-no-key runs text-only with **zero** image/vision calls; context identical to no-vision.
- [ ] `--vision gemini` on a paper with a diagram extracts figures, writes `figure_notes.md`, and those notes appear in the prompt.
- [ ] A forced vision/fetch failure degrades to caption-only with a warning, no abort (NFR7).
- [ ] Figure descriptions cached; a second run makes no new vision calls.

**C. Guardrails & honesty**
- [ ] Allowlist, per-command timeout, output cap, cwd-jail each unit-tested (NFR1–4).
- [ ] File tools reject `..`/absolute paths (unit test).
- [ ] Secrets never printed in logs or transcript.
- [ ] README has the success bar verbatim + the "not a sandbox" note (NFR5).
- [ ] Demo recording (asciinema/GIF): one paper → green smoke-test.

**D. Deployment**
- [ ] `make install` from a fresh clone yields a working binary (DR1).
- [ ] README Quickstart reaches green in ≤5 commands (DR2).
- [ ] Missing key → exit `2` with an actionable message (DR3).

**Verification commands**
```bash
go build ./... && go vet ./... && go test ./...
codemypaper version
codemypaper run 2401.XXXXX --model ollama --verbose                 # offline loop
GEMINI_API_KEY=… codemypaper run <pinned-id> --model gemini         # real green run
GEMINI_API_KEY=… codemypaper run <id-with-diagram> --vision gemini  # writes figure_notes.md
codemypaper run <pinned-id> --model ollama --vision none            # text-only, no vision calls
( cd out/<pinned-id> && python smoke_test.py )                      # exits 0
make install                                                        # fresh clone → working binary
```

## 10. Future (deferred)
- Packaging beyond barebones: prebuilt release binaries, Homebrew tap.
- A real sandbox wired into `run_command`.
- TUI over the same engine.
- More chat backends (Groq/DeepSeek/Claude — same `LLMClient` shape).
- Multimodal agent loop (figures passed directly to a multimodal code model).
- Non-Python targets via `--lang`; lightweight numeric eval; paste-text / local-PDF input.

## 11. CV framing
> **codemypaper** (Go, LLM agents) — A terminal agent that converts arXiv ML papers into runnable reference
> implementations of their core method. Provider-agnostic agent loop with tool-calling, command execution,
> and self-correction; pluggable local (Ollama) / hosted (Gemini) models, with an optional vision pass for
> figure-grounded understanding.
