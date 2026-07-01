# codemypaper — Specification

> The **contract** for v1 and the basis for **final verification**: _what_ codemypaper must do and _how we
> check it_, not how it's built. Implementation lives in [`DESIGN.md`](./DESIGN.md); origin in
> [`PROJECT-BRIEF.md`](./PROJECT-BRIEF.md). §10 is the definition of done — every box checkable against
> behavior, not code shape.

## 1. Overview

`codemypaper` is a Go terminal agent that takes an arXiv ML paper, extracts its **core method**, and
generates a **runnable reference implementation** of it — self-correcting by writing code, running a toy
smoke-test, reading the error, and retrying until the code executes.

**Honest success bar (verbatim in README):**

> _"Generated code runs on toy input and implements the named method"_ — **not** _"reproduces the paper's reported numbers."_

**Stated limitation (also verbatim in README, so it's never a "gotcha"):** the smoke-test is _agent-authored_,
so a green run proves the code is importable and completes a forward pass on toy input **without crashing** —
it does **not** independently prove method fidelity (the agent could, in principle, write a weak test). The
real correctness judge is an ML reader looking at `model.py`. This is the honest, deliberate boundary of the
success bar — the interview answer is "I claim 'runs on toy input,' and I say so out loud; I don't claim
'verified-correct.'"

## 2. Scope

**In (v1):** arXiv ID/URL input · ingest method-focused source text · a **minimal but rigorous** agent loop
that writes/reads files and runs commands, self-correcting on smoke-test failure · generate a small
Python/PyTorch project (core method + toy smoke-test) · swap chat model local (Ollama) ↔ hosted (Gemini) by
flag · emit an output folder + an always-present run summary.

**Out (v1) — deliberately deferred, see §8:** vision/figure understanding · reproducing training
pipelines/datasets/reported numbers · PDF math/figure extraction · TUI · real OS sandboxing (process-level
guardrails only — §6; run only papers you trust) · non-Python output · extra chat backends · wall-clock
budget and JSONL transcript logging.

The agent loop and tool-calling are **intentionally basic** — a single text-based protocol, four tools, one
re-prompt on malformed output. Industrial coding-agent machinery (planning, multi-file diff application,
parallel tool execution, sandboxed execution) is _not_ the goal and would be undefendable as a one-week solo
build. The defensible claim is: "I built the smallest agent loop that genuinely closes the
generate→run→observe→fix cycle, and I specified and tested every part of it."

## 3. Functional requirements

| ID  | Requirement                                                                                                                         |
| --- | ----------------------------------------------------------------------------------------------------------------------------------- |
| FR1 | Accept an arXiv id (`2401.01234`), a full `abs/`/`pdf/` URL, with/without version suffix, and resolve it.                           |
| FR2 | Produce method-focused text trimmed to a char budget; flag when truncated.                                                          |
| FR3 | Drive an act→observe→recover loop where the model can **write/read files** and **run commands**, and signals completion explicitly. |
| FR4 | Generate the method implementation + a toy **smoke-test** exercising it on tiny synthetic input.                                    |
| FR5 | On smoke-test failure, feed the error back and retry, bounded by max-iters.                                                         |
| FR6 | Select the chat backend (`gemini`/`ollama`) at runtime, no code changes.                                                            |
| FR7 | Write an output folder + an always-present run summary (method, entrypoint, iterations, stop reason).                               |
| FR8 | Report success/failure via exit codes (§4).                                                                                         |

Each FR must be defensible in isolation: I should be able to say what it does, why it exists, how it's
verified (§10), and what I deliberately left out.

## 4. CLI contract

```
codemypaper run <arxiv-id-or-url> [flags]
codemypaper version
```

| Flag                  | Default            | Meaning                             |
| --------------------- | ------------------ | ----------------------------------- |
| `--model`             | `gemini`           | Chat backend: `gemini` \| `ollama`. |
| `--gemini-model`      | `gemini-2.5-flash` | Hosted chat model id.               |
| `--ollama-model`      | `qwen2.5-coder:3b` | Local chat model id.                |
| `--out`               | `./out/<arxiv-id>` | Output directory.                   |
| `--max-iters`         | `6`                | Max loop iterations.                |
| `--timeout`           | `120s`             | Per-command timeout.                |
| `--max-context-chars` | `60000`            | Paper-text budget.                  |
| `--verbose`           | `false`            | Stream the loop to stdout.          |

Target language is Python/PyTorch only in v1 (no `--lang` flag — see §8). Every flag has a defensible default;
the per-flag rationale lives in `DESIGN.md`.

**Exit codes:** `0` smoke-test passed · `1` loop ended without a green smoke-test (max-iters reached) · `2`
usage/config error (e.g. missing key) · `3` fatal (fetch failed, backend unreachable). Four codes, each a
distinct, scriptable outcome — no more, no less.

## 5. Configuration & secrets

- Secrets from **env only**, never flags: `GEMINI_API_KEY`, `OLLAMA_HOST` (default `http://localhost:11434`).
- Never logged or written to the `--verbose` stream (redacted).
- No config file in v1; all knobs are §4 flags.

## 6. Non-functional requirements

- NFR1 **cwd-jail** — file/command tools stay within the out dir; `..`/absolute paths rejected.
- NFR2 **allowlist** — only allowlisted command prefixes run; others return an error observation.
- NFR3 **timeouts** — every command bounded by `--timeout`; killed on timeout with a timeout observation.
- NFR4 **output caps** — observations truncated with a marker to protect the context window.
- NFR5 **honesty** — README states the §1 success bar verbatim and that v1 is **not** a sandbox (code runs as
  the invoking user; run only trusted papers).
- NFR6 **robustness** — a malformed model turn triggers one corrective re-prompt, not a crash.
- NFR7 **portability** — macOS and Linux (Windows best-effort).

The four guardrails (NFR1–4) are the most interview-probed part of the project ("how do you stop the LLM
running `rm -rf`?"). Each is small, deliberate, and unit-tested (§10.C).

## 7. Output artifacts (contract)

`out/<arxiv-id>/`:

- Generated source + `smoke_test.py` (+ a deps file like `requirements.txt` if the agent wrote one).
- `RUN_SUMMARY.md` (**always**): id/title, chat backend, method, entrypoint, iterations, final exit code,
  stop reason, caveats, and the §1 success-bar line.

## 8. Deferred (out of v1) — and why deferral is a designed choice

These were in an earlier, larger draft and were **deliberately cut** so v1 stays small enough to build well
and defend fully. Each is a clean, named extension I can articulate in an interview without having built it —
which demonstrates scoping judgment.

- **Vision / figure understanding** — a pre-pass that describes architecture figures into the prompt
  (`VLMClient`, figure parsing, a description cache, a second/third backend). Cut because it roughly doubled
  the surface area and its weakest parts (caching VLM descriptions, two vision backends) were the hardest to
  justify for a one-week build. The `LLMClient` seam is intentionally shaped so a parallel `VLMClient` _could_
  slot in later.
- **Wall-clock budget** (`--wall-budget`) and **JSONL transcript** logging — operational niceties, not core to
  the act→observe→recover thesis; `--max-iters` and a streamed `--verbose` cover v1.
- **Extra chat backends** (Groq/DeepSeek/Claude) — the `LLMClient` interface already makes these a
  one-struct add; building more than two now is breadth without learning value.
- **Real OS sandbox** wired into `run_command`, **non-Python targets** (`--lang`), **TUI**, **paste-text /
  local-PDF input**, **lightweight numeric eval** of the generated code.
- **Packaging beyond barebones** — prebuilt release binaries, Homebrew tap.

## 9. Deployment

Barebones, sized for a ~1-week project. Audience already has Python/torch; the path is **clone +
`make install`**, with `go install` for the no-clone case.

**Prerequisites:** Python 3.10+ on PATH with the generated code's libs (typically `torch`, `numpy`) · Go 1.22+
to build · `GEMINI_API_KEY` for hosted runs · Ollama running only for `--model ollama`.

- DR1 — `make install` / `make build` from a fresh clone yields a working binary (Go modules only).
- DR2 — README Quickstart reaches a green smoke-test in ≤5 commands.
- DR3 — Missing required key → exit `2` with a message naming the env var and suggesting `--model ollama`.

## 10. Acceptance criteria (final-verification checklist)

**A. Build & CLI**

- [ ] `go build ./...`, `go vet ./...`, `go test ./...` pass.
- [ ] `--model` switches ollama ↔ gemini with no code change.
- [ ] `codemypaper version` prints a version; `--help` lists every §4 flag.
- [ ] Exit codes match §4.

**B. Behavior (end-to-end)**

- [ ] `run <id> --model ollama` completes a full act→observe→recover loop with no hosted calls.
- [ ] All FR1 id/URL forms resolve; text is method-focused; ar5iv failure falls back to e-print.
- [ ] A malformed model turn → exactly one corrective re-prompt, not a crash (NFR6).
- [ ] On the **pinned reference paper** (`testdata`), `--model gemini` produces `model.py` + `smoke_test.py` where `python smoke_test.py` exits 0 within `--max-iters`.
- [ ] Self-correction demonstrable: the `--verbose` stream / RUN_SUMMARY shows ≥1 failed smoke-test then a fix that turns it green.
- [ ] `RUN_SUMMARY.md` always written with method, entrypoint, iterations, stop reason.

**C. Guardrails & honesty**

- [ ] Allowlist, per-command timeout, output cap, cwd-jail each unit-tested (NFR1–4).
- [ ] File tools reject `..`/absolute paths (unit test).
- [ ] Secrets never printed in logs or the `--verbose` stream.
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
codemypaper run 2401.XXXXX --model ollama --verbose          # offline loop
GEMINI_API_KEY=… codemypaper run <pinned-id> --model gemini  # real green run
( cd out/<pinned-id> && python smoke_test.py )               # exits 0
make install                                                 # fresh clone → working binary
```
