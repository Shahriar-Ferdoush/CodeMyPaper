# CodeMyPaper — Design & Architecture

> How `codemypaper` is built: architecture, interfaces, internals, and decision rationale.
> The stable contract is in [`PROJECT_SPECIFICATION.md`](../PROJECT_SPECIFICATION.md). Signatures here are
> illustrative, not frozen; this doc changes as the code evolves.

## 1. Architecture

```
arXiv ID ─► cmd/codemypaper (Cobra)        parse flags, resolve backend
              │
              ▼
            internal/arxiv                  ID → method-focused text
              │  paper
              ▼
            internal/pipeline               call #1 generate → write files → run smoke test
              │     uses                    → on failure call #2 debug → write → re-test
              │     └─ internal/llm    LLMClient (ollama / gemini)
              ▼
            out/<arxiv-id>/                 model.py, smoke_test.py, main.py + fetched source
                                            + IMP_DETAILS.md (+ ERROR_LOG.md on failure)
```

**Data flow:** CLI → `arxiv` fetches & trims source → `pipeline` renders the system prompt (reply format +
paper text) → LLM call #1 returns all files in one reply → Go parses, jails, writes them → Go runs
`python3 smoke_test.py` → on failure, LLM call #2 (same conversation + the failing output) returns fixes →
Go re-tests → `IMP_DETAILS.md` always; `ERROR_LOG.md` + exit 1 when no run went green. Control flow lives
entirely in Go; the model is a function called at fixed points, never the driver.

**Carrying abstractions:**
- `LLMClient` — one interface, many chat backends. Selecting a backend is a flag, not a code change.
- The **file-block reply format** (§5) — one parser turns any backend's plain-text reply into a validated set
  of named files. It is the entire surface between model output and code on disk, so every guard
  (path jail, reserved names, required files) hangs off this one seam.

## 2. Design decisions (each: choice · why · rejected alternative)

| # | Question | Decision | Why · rejected alternative |
|---|---|---|---|
| D1 | arXiv fetch | **Three-rung source ladder**, first non-empty wins: official `arxiv.org/html` → `ar5iv` mirror → **e-print LaTeX tarball**; title/abstract always via the arXiv API as a backstop. | Clean section-structured text, no PDF parsing; the official HTML covers ~all new TeX submissions, ar5iv catches papers it misses, e-print guarantees coverage. **Rejected:** parsing the PDF directly — brittle (math, columns, figures) and lower fidelity than the LaTeXML source; ar5iv-first — third-party and lagging, so the official renderer is rung 1. |
| D2 | Long papers | **Section-prioritized + char budget:** keep title/abstract, include method-keyword sections, truncate to `--max-context-chars`, record drops. | Keeps the method signal, fits any context window, deterministic. **Rejected:** embedding-based retrieval — heavier, non-deterministic, overkill for one paper. |
| D3 | Model output format | **Custom delimited file-block reply** parsed by the tool (not provider-native function calling, not JSON). See §5. | Identical across Ollama and Gemini; keeps `LLMClient` plain text-in/out. **Rejected:** provider-native tool APIs — would fork `LLMClient` per backend; JSON with embedded source files — escaping multi-line code inside JSON strings is exactly where small models produce invalid output. |
| D4 | Smoke-test author | **Model writes `smoke_test.py`; the pipeline only runs it.** | The model knows the method signature it just wrote; clean responsibility split. **Rejected:** a fixed harness guessing the API — couples the runner to code it didn't write. |
| D5 | Execution guards | **Fixed command + path jail + timeout + output cap.** The pipeline only ever executes `python3 smoke_test.py`; file paths from model replies are jailed to the out dir and may not collide with pipeline-owned artifacts (`run.log`, `IMP_DETAILS.md`, `ERROR_LOG.md`, the saved source, `paper.meta.json`). | The model never chooses a command, so a command allowlist has nothing to constrain — the guards cover the two model-controlled inputs left: file paths and file content that Python will execute. **Rejected (deferred):** a real OS sandbox — a large project on its own; v1 is labelled "not a sandbox" and the container image (spec §9.4) is the supported isolation path. **Rejected (removed with the loop, D10):** the v0 command allowlist + argument jail — machinery for an agent that picked its own commands. |
| D6 | Output language | **Python/PyTorch only** (no `--lang` flag in v1). | Where ML reference code lives; a single target keeps generation and verification focused. **Rejected:** a language flag with one implementation — speculative generality. |
| D7 | Light eval | **Hard gate:** smoke-test exits 0. **Soft:** the reply's `METHOD:` line names the method → `IMP_DETAILS.md`. | Checkable without overclaiming. **Known limits:** the test is model-authored, so green means "imports + runs a forward pass on toy input," not "method is correct" — a human reader is the real judge; `main.py` is generated but never executed by the pipeline. Both are stated in the README. **Rejected:** numeric reproduction of paper results — out of scope. |
| D8 | Default backend | **`gemini`**; `ollama` is the offline/dev path. | Real runs need a capable model; dev stays free and offline. **Rejected:** defaulting to local — a 3B model cannot implement most methods. |
| D9 | Repair policy | **Single repair attempt + `ERROR_LOG.md`.** First failing smoke-test → error fed back for exactly one fix → rerun; a second failure ends the run with `ERROR_LOG.md` (both failing outputs + what the repair changed) and exit 1. | A second identical failure usually means missing context, not a transient error — further blind retries burn tokens and hide the failure, while one attempt still closes the generate→run→observe→fix cycle and the log keeps the failure resumable. **Rejected:** loop-until-green — replaced for the reason above; zero repair attempts — the observe→fix step is the core of the system. |
| D10 | Orchestration | **Fixed two-call pipeline driven by Go**, replacing the v0 multi-turn tool-calling agent loop: call #1 generates all files in one reply, Go writes and tests them, call #2 (if needed) repairs. At most one corrective re-prompt per call for a malformed reply. | The task graph is fully known in advance (generate → test → one repair → test → exit); known structure ⇒ code-orchestrated workflow, agent loops are for tasks whose steps can't be predicted. The pipeline is deterministic (exactly 1–2 LLM calls), every failure is attributable to a stage, and the empirical case was decisive: given loop freedom, the v0 agent burned all 6 turns running a smoke test it never wrote. Accepted trade-offs: (a) the debug call continues the generate conversation, re-sending paper-sized context once — a paper-blind "files + error" repair can't fix bugs rooted in misreading the method; (b) no mid-run `pip install` — spec §9.1 assumes torch/numpy preinstalled. **Rejected:** the agent loop (tool registry, per-turn tool calls, turn cap) — standard for open-ended coding agents, but this task isn't open-ended; the loop bought nondeterminism with no added capability. |
| D11 | Rerun caching | **Reuse the persisted paper source across reruns of the same id, fully offline.** A `paper.meta.json` sidecar records the winning rung plus title/abstract next to the saved source; a run with a matching sidecar + source skips `Fetch` entirely and rebuilds `*Paper` by re-running that rung's pure parse step on the cached bytes. `--refetch` forces a fresh fetch, which also clears any other rung's leftover file so exactly one source + sidecar ever coexist. | Reruns of the same id (a different `--model`, iterating on the pipeline) don't need the paper re-fetched. Caching raw bytes + a few strings (not parsed `Sections`) keeps the sidecar tiny; re-parsing cached bytes is cheap and deterministic. **Known limit:** the cache is keyed on the bare id, not the version — a revised (`vN`) paper serves stale content until `--refetch`. **Rejected:** always re-fetching; a cache that still calls the metadata API every run (a cache hit should be genuinely offline); timestamped output subdirectories (changes the `--out` contract without addressing re-fetching). |
| D12 | Release supply chain | **Two separate signed-and-SBOM'd pipelines on one `v*` tag, both keyless (cosign + GitHub OIDC).** GoReleaser (`.goreleaser.yml`) builds darwin/linux × amd64/arm64 binaries, archives them, emits a syft SBOM per archive, and cosign-signs the checksums file. A second job (`release.yml`) builds the container image natively on matched amd64/arm64 runners (no QEMU), pushes the multi-arch index to **both GHCR and Docker Hub**, cosign-signs each registry's index digest — never the per-arch children, since `cosign verify <tag>` resolves through the index — and attaches an image SBOM via `cosign attest` (a second, separate SBOM: the image adds the Debian base layer and the baked-in torch/numpy wheels that the binary archives don't have). SLSA build-provenance attestation and OpenSSF Scorecard are excluded. | An SBOM makes future CVE exposure answerable by inspection; a signature binds each artifact to this repository's CI rather than to a long-lived key that could leak — near-zero marginal cost via GoReleaser for binaries, one extra job for the image. Two registries because they answer different questions: GHCR needs no long-lived secret and sits beside the code, Docker Hub is where `docker pull <image>` works with no registry prefix — one build, one registry-to-registry copy, not a second build. Provenance/Scorecard depend on consumers running verifiers; this tool's consumers install via `go install` or `docker pull` and do not. **Rejected:** key-based signing — custody/rotation burden with no benefit over the OIDC identity; per-binary signatures — the signed checksums file already covers them; signing per-architecture images instead of the index — the tag-verify path would find nothing. |
| D13 | CI & lint gate | **Curated golangci-lint set** (`govet`, `staticcheck`, `errcheck`, `revive`, `gosec`, `gofmt`/`goimports`) over enable-all, run via GitHub Actions on push + PR alongside `go test -race -cover`. `govulncheck` (call-graph-aware) gates a separate job; Dependabot watches both the `gomod` and `github-actions` ecosystems. Actions are pinned to major-version tags, not commit SHAs. | Each linter earns its place instead of training developers to ignore a noisy gate; call-graph-aware scanning flags only vulnerabilities in code actually reached, not every version-matched CVE. GitHub Actions is co-located with the repo and free for public repos — a second CI system adds an operational cost with no added signal. **Accepted trade-off:** tag-pinning leaves the (small) exposure of a compromised or retagged action changing what CI runs; judged acceptable here — two GitHub-owned actions plus one third-party linter action, no secrets in scope. **Rejected:** enable-all linting — noise trains developers to ignore the gate; SHA-pinning every action — the defense this trade-off explicitly declines. |

### Threat model for the guardrails (D5)

- **What the guards constrain:** *which* command runs (exactly `python3 smoke_test.py`, chosen by Go, never by
  the model), *where* model-named files land (the out-dir path jail + the reserved-name check), *how long* the
  test may run (timeout), and *how much* output is captured (cap).
- **What they do NOT constrain:** what the generated Python does internally. `smoke_test.py` runs with the
  full privileges of the invoking user and can spawn subprocesses, do network I/O, and write anywhere the
  user can. The path jail restricts where the *pipeline* writes model-named files; it does not sandbox
  file I/O performed from inside the running Python process.
- **Threat model:** bound blast radius and runtime against accidental or naive damage; explicitly not a
  defense against a malicious paper author — hence the README's "not a sandbox — run only papers you trust"
  (NFR5). Kernel-level isolation is provided by the container image (spec §9.4), not reimplemented in-process.

## 3. Components

### 3.1 `internal/llm`
```go
type Role string // system | user | assistant
type Message struct { Role Role; Content string }

// Single seam to any chat backend. The file-block reply format lives above this layer (§5).
type LLMClient interface {
    Chat(ctx context.Context, messages []Message) (string, error)
    Name() string
}
```
- `ollama.go` — POST `${OLLAMA_HOST}/api/chat`, `stream:false`.
- `gemini.go` — Generative Language API; no system role → fold the system text into the first user turn.
- All honor `ctx`; typed errors (`ErrNoAPIKey`, `ErrBackendUnreachable`, `ErrRateLimited`).

### 3.2 `internal/pipeline` internals
```go
type File struct { Name, Content string }
type Reply struct { Method string; Files []File }

func parseReply(raw string) (Reply, error)                       // pure string → value (§5)
func runSmokeTest(ctx, dir, timeout, logger) testResult          // the one command Go ever runs
func safeJoin(base, rel string) (string, error)                  // the path jail
```
- `parseReply` is pure (no I/O, no outDir knowledge) — the jail and reserved-name checks run at write time,
  where that context lives. Its error messages double as the corrective re-prompt text.
- `Files` is a slice, not a map: write order stays deterministic and duplicate names stay visible.
- All failure modes of the smoke-test run (non-zero exit, timeout, missing interpreter) fold into one
  `testResult`, so the verdict is a single `passed()` check.

### 3.3 `internal/arxiv`
```go
type Paper struct {
    ID, Title, Abstract string
    Sections  []Section
    Source    string // "arxiv-html" | "ar5iv" | "eprint" | "api-only"
    Raw     []byte // exact body of the winning rung, as served (nil for api-only)
    RawName string // filename for Raw: "paper.html" | "paper.tar.gz" | "paper.tex.gz"
}
type Section struct{ Heading, Body string; MethodRelevant bool }
type CachedMeta struct{ Source, RawName, Title, Abstract string } // the paper.meta.json shape

func Fetch(ctx context.Context, idOrURL string) (*Paper, error)
func FromCache(id string, meta CachedMeta, raw []byte) (*Paper, error)
func (p *Paper) PromptText(maxChars int) (string, bool)
```
- `ParseID` normalizes FR1 forms. `Fetch` runs the D1 ladder, recording which rung won in `Source`;
  `"api-only"` means every rung failed and only the Atom title/abstract backstop survived. It errors only
  when the backstop is empty too.
- `FromCache` (D11) is `Fetch`'s offline counterpart: given the sidecar metadata and the raw bytes already
  on disk, it dispatches on `RawName` to the same pure parse step `Fetch` would have used for that rung
  (`htmlToSections` for HTML, `extractEprintTeX`+`latexSections` for e-print) and rebuilds an identical
  `*Paper` with no network call. `main.go` owns reading the sidecar/raw file and deciding cache-hit vs.
  fetch, consistent with `arxiv` staying free of filesystem I/O.
- `MethodRelevant` = case-insensitive substring match of a heading against a fixed keyword set:
  `{method, approach, model, architecture, algorithm, framework, proposed, technical}`. Deterministic; the
  trade-off (vs embedding retrieval, D2) is that non-standard section names can be missed — the abstract is
  always included as a backstop so a missed heading never drops all method signal.
- The winning rung's exact bytes ride on `Paper` (`Raw`/`RawName`) rather than a write inside `Fetch`: the
  CLI persists them into the out dir for inspection, keeping `arxiv` free of filesystem I/O.
- `PromptText` applies the D2 budget and reports truncation via its second return value. `arxiv` imports
  no other internal package — it's a pure ingestion library, testable in isolation.

### 3.4 `internal/pipeline` API
```go
type Config struct { OutDir string; TestTimeout time.Duration; MaxContextChars int }
type Outcome struct {
    Success bool
    Method  string // from the reply's METHOD line
    StopReason string // passed | repair_exhausted | malformed_reply | fatal_error
    FirstFail, SecondFail string // captured failing test outputs
}
func Run(ctx context.Context, client llm.LLMClient, paper *arxiv.Paper, cfg Config, logger *log.Logger) (Outcome, error)
```
The returned `error` is non-nil only for fatal chat-backend failures; every other ending is encoded in
`Outcome`, and `IMP_DETAILS.md` is written on every ending.

## 4. The pipeline (normative)

The run is a fixed sequence (D10), with the D9 single-repair policy as its shape:

```
messages := [system(format + paper text), user(task)]
reply    := chatForFiles(#1 generate)        // parse + validate; ≤1 corrective re-prompt
             └─ malformed twice → ERROR_LOG.md (raw reply) → exit 1
writeFiles(reply)                            // path jail + reserved-name check
first := runSmokeTest()
if first green → IMP_DETAILS.md → exit 0
messages += [assistant(reply), user(first failing output)]
fixed := chatForFiles(#2 debug)              // may rewrite any already-written file, no new ones
             └─ malformed twice → ERROR_LOG.md (raw reply + first failure) → exit 1
writeFiles(fixed)
second := runSmokeTest()
if second green → IMP_DETAILS.md → exit 0
ERROR_LOG.md (both failing outputs + which files the repair rewrote) → IMP_DETAILS.md → exit 1
```

- **A run is exactly 1 or 2 logical LLM calls**, plus at most one corrective re-prompt each. There is no turn
  budget because there are no turns for a model to wander through.
- **The debug call continues the generate conversation** — the model keeps the paper text and its own files in
  context; only the failing output is new information (trade-off recorded in D10).
- **`ERROR_LOG.md` is written on every non-green, non-fatal ending** and always carries whatever failing test
  output exists, so the failure stays resumable. A stale `ERROR_LOG.md` from a previous run is removed at run
  start so a green rerun can't report two contradicting endings.
- **StopReason:** `passed | repair_exhausted | malformed_reply | fatal_error` (§3.4).

## 5. File-block reply format (D3)

Both calls must reply with a `METHOD:` line and one delimited block per file, nothing else:

```
METHOD: <short name of the paper's core method>

=== FILE: model.py ===
<file content>
=== END FILE ===
```

**Parser rules:**
- Marker lines match only as the **entire line, exact text** — a line merely *containing* the marker (a
  docstring mentioning the format) is file content, so model content can't truncate a file.
- One outer markdown fence around the whole reply is stripped defensively — models add one even when told not to.
- The generate call must produce `model.py`, `smoke_test.py`, `main.py`; extras (e.g. `requirements.txt`) are
  allowed. The debug call may only rewrite files that already exist — a repair that invents new files is
  answering a different question.
- Every file name passes the path jail (`safeJoin`: no `..`, no absolute) and the reserved-name check
  (pipeline-owned artifacts can't be overwritten by model content).
- Any violation ⇒ the reply is malformed ⇒ one corrective re-prompt whose text is the validation error itself;
  a second malformed reply ends the run.

## 6. Prompt (`pipeline/prompt.md`)
System prompt order: (1) role — implement the *core method* as minimal Python/PyTorch; (2) the success
criterion; (3) the §5 reply format with the three required files spelled out; (4) hard rules — tiny synthetic
tensors in `smoke_test.py` asserting finite/correct shape, exit 0/non-zero, torch/numpy only, relative paths;
(5) `paper.PromptText()` delimited (+ truncation notice).

First user message: *"Implement the core method of arXiv:<id> (<title>). Reply with the METHOD line and the
required file blocks."* The debug message carries the failing output and exit code; the corrective re-prompt
carries the parse/validation error verbatim.

## 7. Layout
```
codemypaper/
├─ go.mod                         # module github.com/shahriar-ferdoush/codemypaper
├─ Makefile                       # build / install / test / lint
├─ .goreleaser.yml                # binary/archive build, SBOM, checksum signing (D12)
├─ .golangci.yml                  # curated lint set (D13)
├─ Dockerfile / .dockerignore     # container image (D12)
├─ LICENSE
├─ .github/
│  ├─ workflows/{go.yml, release.yml}  # CI gate (D13); release + image publish (D12)
│  └─ dependabot.yml              # gomod + github-actions
├─ cmd/codemypaper/main.go        # Cobra entry; flag wiring; backend resolution
├─ internal/
│  ├─ pipeline/ { pipeline.go, parse.go, prompt.go, run.go, jail.go, output_imp_details.go }
│  ├─ llm/      { client.go, ollama.go, gemini.go }
│  ├─ arxiv/    { fetch.go, parse.go, paper.go, eprint.go, html.go, cache.go }
│  └─ log/      { log.go }        # stderr logger (Debugf/Infof/Warnf/Errorf); AttachFile tees to run.log
├─ testdata/                      # cached paper fixtures for offline tests
└─ README.md                      # scope, Quickstart, security model
```

## 8. Deployment (implements spec §9)
**Makefile:** `build` (`go build -ldflags "-X main.version=$(VERSION)" -o bin/codemypaper ./cmd/codemypaper`)
· `install` (same ldflags, `go install ./cmd/codemypaper`) · `test` (`go test ./...`) · `lint` (`go vet ./...`,
+ golangci-lint if present). `VERSION` derives from `git describe`.

**`go install`:** `go install github.com/shahriar-ferdoush/codemypaper/cmd/codemypaper@latest`.

The README quickstart runs the binary from `./bin` rather than `make install` because `go install`'s
target (`$(go env GOPATH)/bin`) is not on PATH in a default shell — the `./bin` path works with
zero environment setup; `make install` + the PATH line is documented as the alternative.

**CI (D13):** `go.yml` runs on every push and pull request — `make fmt-check`, `make build`, `go test -race
-cover ./...`, and lint via the official `golangci-lint-action` — on an `[ubuntu-latest, macos-latest]`
matrix, plus a separate `govulncheck` job.

**Container (D12):** `docker run --rm -v $PWD/out:/work/out ghcr.io/shahriar-ferdoush/codemypaper:vX.Y.Z run
<arxiv-id>` (or the `docker.io/shahriarferdoush/codemypaper` equivalent). Ollama is not bundled in the image
— `--model ollama` needs `OLLAMA_HOST=http://host.docker.internal:11434` to reach a server running on the
host.
