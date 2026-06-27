# codemypaper — Design & Architecture

> *How* codemypaper is built: architecture, interfaces, internals, decision rationale, layout, build order.
> The stable contract is in [`PROJECT_SPECIFICATION.md`](./PROJECT_SPECIFICATION.md). Signatures here are
> illustrative, not frozen; this doc changes as the code evolves.

## 1. Architecture

```
arXiv ID ─► cmd/codemypaper (Cobra)        parse flags, resolve backends, wire Agent
              │
              ▼
            internal/arxiv                  ID → method-focused text (+ figures)
              │  paper (+ optional figure notes via vision)
              ▼
            internal/agent (THE loop)       build prompt → Chat → parse → run → observe
              │     uses
              │     ├─ internal/llm    LLMClient (ollama/gemini) + VLMClient (gemini/groq)
              │     └─ internal/tools  Tool + registry: write / run / read / finish
              ▼
            out/<arxiv-id>/                 generated project + RUN_SUMMARY.md
```

**Data flow:** CLI → `arxiv` fetches & trims source + figures → optional `vision` annotates figures →
`agent` builds the system prompt (tools + paper text + task) → loop: `llm.Chat` → parse → `tools` run →
append observation → until `finish` / smoke-test green / budget out → write `RUN_SUMMARY.md`.

**Carrying abstractions:** `LLMClient` (one interface, many chat backends) · `VLMClient` (parallel, optional,
backend-independent) · `Tool` + registry (the agent's hands; add a capability = register one struct).

## 2. Design decisions (resolving brief §8)

| # | Question | Decision | Why |
|---|---|---|---|
| D1 | arXiv fetch | **ar5iv HTML** → text; fallback **e-print LaTeX tarball**; title/abstract via arXiv API. | Clean section-structured text, no PDF parsing; tar is the always-available backstop. |
| D2 | Long papers | **Section-prioritized + char budget:** keep title/abstract, include method-keyword sections, truncate to `--max-context-chars`, record drops. | Keeps the method signal; fits small and large context windows; deterministic. |
| D3 | Tool-calling | **Custom single-block JSON protocol** we parse (not provider-native). See §5. | Identical across backends; keeps `LLMClient` plain text-in/out; teaches a parser. |
| D4 | Smoke-test author | **Agent writes `smoke_test.py`; harness only runs it.** | Agent knows the method signature; clean responsibility split. |
| D5 | `run_command` guards | **Allowlist + per-command timeout + cwd-jail + output cap.** Prefixes: `python(3)`, `pip(3)`, `ls`, `cat`, `pytest`. | Bounds blast radius without a full sandbox. |
| D6 | Output language | **Python/PyTorch only**, behind `--lang`. | Where ML reference code + verifiability live; flag keeps it open. |
| D7 | Light eval | **Hard gate:** smoke-test exits 0. **Soft:** `finish` names method + symbol → `RUN_SUMMARY.md`. | Checkable without overclaiming. |
| D8 | Default backend | **`gemini`**; `ollama` the offline/dev path. | Real runs are the product; dev stays free. |
| D9 | Vision usage | **Ingestion pre-pass, not a multimodal loop:** `VLMClient` describes figures into text augmenting `PromptText`. | Works with a text-only local code model; cacheable artifact; decoupled from chat backend. |
| D10 | Vision optional | **`--vision auto\|gemini\|groq\|none`** (auto rules in spec §4). | "No VLM → just the chat model." Free-tier friendly; swappable. |

> **Confirm at build time:** model ids `gemini-2.5-flash` and Groq `llama-3.2-11b-vision` (LLaVA variants
> exist) against live provider lists.

## 3. Components

### 3.1 `internal/llm`
```go
type Role string // system | user | assistant
type Message struct { Role Role; Content string }

// Single seam to any chat backend. Tool-calling lives above this layer (§5).
type LLMClient interface {
    Chat(ctx context.Context, messages []Message) (string, error)
    Name() string
}

// Optional, parallel vision seam.
type Image struct { Data []byte; MIMEType string; Ref string }
type VLMClient interface {
    Describe(ctx context.Context, instruction string, images []Image) (string, error)
    Name() string
}
```
- `ollama.go` — POST `${OLLAMA_HOST}/api/chat`, `stream:false`.
- `gemini.go` — Generative Language API; no system role → fold into first user turn.
- `gemini_vision.go` — `inline_data` parts · `groq_vision.go` — OpenAI-compatible `image_url` data-URIs.
- All honor `ctx`; typed errors (`ErrNoAPIKey`, `ErrBackendUnreachable`, `ErrRateLimited`); vision failures
  are non-fatal.

### 3.2 `internal/tools`
```go
type Tool interface {
    Name() string
    Description() string
    Schema() map[string]string
    Run(ctx context.Context, args map[string]any) (Result, error)
}
type Result struct { Output string; IsError bool; ExitCode int }
type Registry struct{ /* name -> Tool */ }
```
| Tool | Args | Behavior |
|---|---|---|
| `write_file` | `path`, `content` | mkdir parents, write inside cwd-jail; reject `..`/absolute. |
| `run_command` | `cmd` | allowlist + cwd-jail + timeout; returns combined stdout/stderr (capped) + exit code. |
| `read_file` | `path` | read a file in the out dir. |
| `finish` | `summary`, `method`, `entrypoint` | terminates loop; seeds `RUN_SUMMARY.md`. |

### 3.3 `internal/arxiv`
```go
type Paper struct {
    ID, Title, Abstract string
    Sections    []Section
    Figures     []Figure
    FigureNotes []FigureNote // filled by vision; may be empty
    Source      string       // "ar5iv" | "eprint"
    Truncated   bool
}
type Section struct{ Heading, Body string; MethodRelevant bool }
type Figure struct{ Ref, Caption, ImageURL, Section string; MethodRelevant bool }
type FigureNote struct{ Ref, Description string }

func Fetch(ctx context.Context, idOrURL string, maxChars int) (*Paper, error)
func (p *Paper) PromptText() string
func (f *Figure) FetchImage(ctx context.Context) ([]byte, string, error)
```
- `ParseID` normalizes FR1 forms. `Fetch` tries ar5iv, falls back to e-print, errors only if both fail.
- Figures parsed from ar5iv `<figure>`/`<figcaption>`; `MethodRelevant` on diagram keywords (architecture,
  overview, framework, pipeline, model, block, diagram, schematic). E-print figures (PDF/EPS) unsupported —
  ar5iv is the figure source.
- `PromptText` applies the D2 budget, appends a figure-notes block when present, sets `Truncated`. `arxiv`
  imports neither `llm` nor `vision`.

### 3.4 `internal/vision` (D9/D10)
```go
// Annotate fills paper.FigureNotes. vlm == nil ⇒ no-op. Per-figure failures logged, never fatal.
func Annotate(ctx context.Context, vlm llm.VLMClient, paper *arxiv.Paper, maxFigs int, cacheDir string) error
```
- Picks `MethodRelevant` figures by proximity to method sections, capped at `--max-figures`.
- Per figure: `FetchImage` → `vlm.Describe` with: *"You are reading a figure from an ML paper to help
  re-implement its method. Caption: «caption». Describe the components, data flow, ordering, connections, and
  any tensor shapes/operations shown, concretely enough to write code. Ignore styling."*
- **Cache:** `sha256(image bytes + instruction)` under `cacheDir`; re-runs reuse. Imports `arxiv` + `llm`.

### 3.5 `internal/agent`
```go
type Config struct { MaxIters int; CmdTimeout, WallBudget time.Duration; Lang string; Verbose bool }
type Agent struct { llm llm.LLMClient; tools *tools.Registry; cfg Config }
type Outcome struct {
    Success bool; Iterations int
    Method, Entrypoint, Summary string
    StopReason string // finished | max_iters | wall_budget | fatal_error
}
func (a *Agent) Run(ctx context.Context, paper *arxiv.Paper, outDir string) (Outcome, error)
```

## 4. The loop (normative)
```
messages := [system(promptWithToolsAndPaper), user(task)]
for iter := 1; iter <= cfg.MaxIters; iter++ {
    if wallBudgetExceeded() { stop("wall_budget"); break }
    raw, err := llm.Chat(ctx, messages); if err != nil { return fatal(err) }
    messages.append(assistant(raw))
    call, perr := parseToolCall(raw)
    if perr != nil { messages.append(user(protocolReminder(perr))); continue }  // one re-prompt
    if call.Name == "finish" { stop("finished"); record(call); break }
    res, _ := tools.Run(ctx, call.Name, call.Args)        // act
    messages.append(user(observation(res)))               // observe
}
writeRunSummary(outDir, outcome)
```
- **Stop (first wins):** `finish` · max-iters · wall-budget · fatal-after-retry.
- **Malformed:** one corrective re-prompt per bad turn (counts against max-iters), restating the protocol.

## 5. Tool-call protocol (D3)
Model replies with optional prose **then exactly one** fenced `json` block with a `tool` key; parser takes the
**last** such block.
````
```json
{ "tool": "write_file", "args": { "path": "model.py", "content": "import torch..." } }
```
````
Finish: `{ "tool": "finish", "args": { "summary": "...", "method": "...", "entrypoint": "smoke_test.py" } }`

**Parser:** tolerate prose; require valid JSON; reject multiple blocks; unknown `tool` → corrective reminder.

## 6. Prompt (`agent/prompt.go`)
System prompt order: (1) role — implement the *core method* as minimal Python/PyTorch; (2) the honest success
bar; (3) tool catalog + §5 protocol with one example; (4) hard rules — minimal deps, write `smoke_test.py` on
tiny synthetic tensors asserting finite/correct shape, run via `run_command`, fix on failure, `finish` only
after green; (5) `paper.PromptText()` delimited (+ truncation notice; figure notes marked authoritative for
architecture/shape).

First user message: *"Implement the core method of arXiv:<id> (<title>). Produce `model.py` and
`smoke_test.py`. Iterate until the smoke-test passes, then `finish`."*

## 7. Layout
```
codemypaper/
├─ go.mod
├─ Makefile                       # build / install / test / lint
├─ cmd/codemypaper/main.go        # Cobra entry; flag wiring; backend resolution
├─ internal/
│  ├─ agent/   { loop.go, prompt.go, parse.go }
│  ├─ llm/     { client.go, ollama.go, gemini.go, vision.go, gemini_vision.go, groq_vision.go }
│  ├─ vision/  { describe.go }     # Annotate (+cache); no-op if VLM nil
│  ├─ tools/   { tools.go, writefile.go, runcmd.go, readfile.go, finish.go }
│  └─ arxiv/   { fetch.go, parse.go, figures.go, paper.go }
├─ testdata/                      # cached paper/figure fixtures for offline tests
└─ README.md                      # honest scope + Quickstart + demo
```

## 8. Deployment (implements spec §8)
**Makefile:** `build` (`go build -ldflags "-X main.version=$(VERSION)" -o bin/codemypaper ./cmd/codemypaper`)
· `install` (same ldflags, `go install ./cmd/codemypaper`) · `test` (`go test ./...`) · `lint` (`go vet ./...`,
+ golangci-lint if present).

**`go install`:** module `github.com/<owner>/codemypaper`; `go install …/cmd/codemypaper@latest`.

**README Quickstart (DR2):**
```
git clone https://github.com/<owner>/codemypaper && cd codemypaper
make install
export GEMINI_API_KEY=…
codemypaper run 2401.XXXXX
( cd out/2401.XXXXX && python smoke_test.py )   # exits 0
```

## 9. Build order (Go learning map)

| Milestone | Deliverable | Go concepts | Done when |
|---|---|---|---|
| M1 | CLI skeleton + `LLMClient` + Ollama; one round-trip | structs, interfaces, packages, `net/http`, JSON | `run` prints a reply from local Ollama |
| M2 | `Tool` + registry + `write_file`/`run_command` + loop | interfaces, registry map, `os/exec`, errors | agent writes a file & runs a command via the protocol |
| M3 | `arxiv` fetch + real prompt; swap to Gemini | `context`, HTTP clients, strings | a real id produces method-focused text |
| M3.5 *(opt)* | Vision pre-pass: `VLMClient` + `Annotate` + figures | second interface behind a flag, `image/*`, content-hash cache, graceful degrade | `--vision gemini` yields `figure_notes.md`; `none` matches text path |
| M4 | Self-correction + packaging (Makefile, README, demo) | loops, `select`/timeouts, cleanup | a known paper → green smoke-test in budget, `make install` works |
