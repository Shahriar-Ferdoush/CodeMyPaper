# CodeMyPaper — Project Specification

Requirements contract for v1: what the system must do and how that is verified. Architecture and
design rationale are in [`DESIGN.md`](./docs/DESIGN.md). Section 10 is the definition of done; every
criterion in it is checkable against observable behavior rather than code structure.

## 1. Overview

`codemypaper` takes an arXiv machine-learning paper, extracts its core method, and generates a
runnable Python/PyTorch reference implementation of that method. It operates as a fixed pipeline
with a single-repair policy: one model call generates the implementation, a smoke test, and an
entrypoint; the tool writes the files and runs the test on toy input; if the test fails, a second
model call receives the error output for exactly one repair attempt before the test is rerun. A
run whose rerun also fails ends with a structured error log (`ERROR_LOG.md`) rather
than further retries, which show diminishing returns against the same error; the log preserves
everything needed to continue the fix.

**Success criterion.** A run is successful when the generated code executes on toy input and
implements the paper's named method. Reproducing the paper's reported experimental results is
explicitly not a goal.

**Known limitation.** The smoke test is authored by the model itself. A passing run therefore
demonstrates that the generated code imports cleanly and completes a forward pass on toy input
without error; it does not independently verify fidelity to the paper's method. Final judgment of
correctness rests with a human reader of the generated source. This limitation is stated in the
README so users can weigh results accordingly.

## 2. Scope

### 2.1 In scope (v1)

- Accept an arXiv identifier or URL as input and resolve it.
- Ingest method-focused source text for the paper. HTML sources are preferred, with the LaTeX
  e-print archive as a fallback; PDF parsing is excluded by design.
- Drive a fixed generate–test–repair pipeline: the tool writes the model's files, runs the smoke
  test itself, and grants a single repair attempt when the test fails.
- Generate a small Python/PyTorch project containing the core method and a toy smoke test.
- Select the chat backend — local (Ollama) or hosted (Gemini) — at runtime via a flag.
- Emit an output directory and a run summary for every run, successful or not.
- Automated quality gates, versioned binary releases, and a container image (section 9).

### 2.2 Out of scope (v1)

Figure and vision understanding; reproduction of training pipelines, datasets, or reported numbers;
PDF text or math extraction; in-process OS sandboxing; output languages other than Python; chat backends beyond the two above; a terminal UI;
wall-clock budgets and transcript logging; Homebrew distribution. Section 8 lists these with the
reason each was excluded.

The pipeline is deliberately minimal: at most two model calls per run, a single text-based reply
format, and one corrective re-prompt per call on malformed model output. Agentic tool-calling
loops, planning phases, and multi-file diff application are not v1 goals.

## 3. Functional requirements

| ID  | Requirement                                                                                                                                                                                                       |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| FR1 | Accept an arXiv id (`2401.01234`) or a full `abs/`/`pdf/` URL, with or without a version suffix, and resolve it.                                                                                                  |
| FR2 | Produce method-focused paper text trimmed to a configurable character budget, flagging when truncation occurred.                                                                                                  |
| FR3 | Obtain all generated files from a single model reply in a delimited file-block format, validate them (required files present, paths confined per NFR1), and write them to the output directory.                   |
| FR4 | Generate the method implementation (`model.py`), a smoke test that exercises it on small synthetic input (`smoke_test.py`), and a demo entrypoint (`main.py`).                                                    |
| FR5 | On smoke-test failure, feed the error output back to the model for exactly one repair attempt, then rerun the test; if the rerun also fails, end the run, write `ERROR_LOG.md` (section 7), and exit with code 1. |
| FR6 | Select the chat backend (`gemini` or `ollama`) at runtime with no code changes.                                                                                                                                   |
| FR7 | Write an output directory and an always-present run report (method, entrypoint, test verdicts, stop reason).                                                                                                      |
| FR8 | Report the run outcome through process exit codes (section 4).                                                                                                                                                    |

## 4. Command-line interface

```
codemypaper run <arxiv-id-or-url> [flags]
codemypaper version
```

| Flag                  | Default            | Meaning                                                           |
| --------------------- | ------------------ | ----------------------------------------------------------------- |
| `--model`             | `gemini`           | Chat backend: `gemini` or `ollama`.                               |
| `--gemini-model`      | `gemini-3.1-flash-light` | Hosted chat model id.                                       |
| `--ollama-model`      | `qwen2.5-coder:3b` | Local chat model id.                                              |
| `--out`               | `./out/<arxiv-id>` | Output directory.                                                 |
| `--timeout`           | `120s`             | Smoke-test timeout.                                               |
| `--max-context-chars` | `60000`            | Paper-text budget.                                                |
| `--verbose`           | `false`            | Stream pipeline progress to stderr.                               |
| `--refetch`           | `false`            | Force a fresh paper fetch, bypassing a cached source (section 7). |

The generation target is Python/PyTorch only in v1; there is no `--lang` flag (section 8). There
is no iteration cap: a run makes at most two model calls (generate and repair), each with at most
one corrective re-prompt, so the request count is bounded by construction. Per-flag default
rationale is documented in `docs/DESIGN.md`.

**Exit codes.** Each code is a distinct, scriptable outcome:

| Code | Meaning                                                                                                                                              |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0    | Smoke test passed.                                                                                                                                   |
| 1    | Run ended without a passing smoke test (repair attempt exhausted, or a model reply unusable after its corrective re-prompt); `ERROR_LOG.md` written. |
| 2    | Usage or configuration error (for example, a missing API key).                                                                                       |
| 3    | Fatal error (paper fetch failed, backend unreachable).                                                                                               |

## 5. Configuration and secrets

- The only secret is `GEMINI_API_KEY`. It is read from the environment, never from a flag, and
  must never appear in logs or in the `--verbose` stream.
- `OLLAMA_HOST` is non-secret configuration, read from the environment with a default of
  `http://localhost:11434`.
- There is no configuration file in v1; all other knobs are the section 4 flags.

## 6. Non-functional requirements

| ID   | Requirement                                                                                                                                                                                                                                                                                  |
| ---- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| NFR1 | **Path confinement.** Model-named files are written only inside the output directory; `..` and absolute paths are rejected, as are names that collide with the tool's own artifacts (`run.log`, `IMP_DETAILS.md`, `ERROR_LOG.md`, the saved paper source, the cache metadata file).          |
| NFR2 | **Fixed command.** The tool itself decides what executes — exactly `python3 smoke_test.py` in the output directory; the model never selects a command.                                                                                                                                       |
| NFR3 | **Timeouts.** The smoke-test run is bounded by `--timeout` and killed on expiry, with the timeout recorded in the captured output.                                                                                                                                                           |
| NFR4 | **Output caps.** Captured test output is truncated with an explicit marker to protect the model's context window.                                                                                                                                                                            |
| NFR5 | **Transparency.** The README states the section 1 success criterion and limitation verbatim, and states that these guardrails are process-level controls, not a sandbox: generated code runs as the invoking user, so only trusted papers should be run outside the container (section 9.4). |
| NFR6 | **Robustness.** A malformed model reply triggers one corrective re-prompt, never a crash; a second malformed reply ends the run with `ERROR_LOG.md` and exit code 1.                                                                                                                         |
| NFR7 | **Portability.** macOS and Linux are supported; Windows is best-effort.                                                                                                                                                                                                                      |

NFR1–NFR4 are the security boundary of the bare CLI; their verification is defined in section
10.C. The container image (section 9.4) is the supported way to add kernel-level isolation on top
of them.

## 7. Output artifacts

Every run produces `out/<arxiv-id>/` containing:

- The generated source (`model.py`, `main.py`) and `smoke_test.py`, plus a dependency file (for
  example `requirements.txt`) when the model writes one. Only `smoke_test.py` is executed by the
  run; `main.py` is a demo entrypoint for the reader.
- The fetched paper source, exactly as served by the winning ingestion source: `paper.html`
  (HTML sources) or `paper.tar.gz`/`paper.tex.gz` (e-print). Omitted when only API metadata was
  available. Saving it lets extraction quality be inspected independently of model output.
- `paper.meta.json`, a small sidecar recording which source rung won plus the paper's title and
  abstract. A later run for the same id reuses the saved source and this sidecar instead of
  fetching again, unless `--refetch` is given; a run that does fetch fresh replaces both files, so
  at most one paper source is ever kept.
- `IMP_DETAILS.md`, written on success and failure alike: paper id and title, chat backend, method
  name, entrypoint, files written, each smoke-test verdict, stop reason, final exit code, and the
  section 1 success criterion.
- `ERROR_LOG.md`, written when the run ends without a passing smoke test. After an exhausted
  repair attempt it contains the first failing smoke-test output, what the repair changed, and the
  second failing output; after an unusable model reply it contains the raw reply plus any failing
  test output that preceded it — in both cases structured so that a reader (or a later session
  given the file as context) can continue the fix from the log alone. A stale `ERROR_LOG.md` from
  a previous run is removed at run start.

## 8. Exclusions and deferred features

The following were considered and excluded from v1 to keep the system small enough to build and
verify within its schedule.

- **Figure/vision understanding.** A pre-pass describing architecture figures into the prompt
  would roughly double the system's surface area (a second model interface, figure extraction, a
  description cache). The `LLMClient` interface is shaped so a parallel vision client could be
  added without restructuring.
- **Wall-clock budget and transcript logging.** Operational conveniences, not core to the
  generate–test–repair pipeline; the bounded call count and `--verbose` cover v1's needs.
- **Additional chat backends.** The backend interface makes each new provider a single-struct
  addition; two backends (one local, one hosted) are sufficient to demonstrate backend
  independence.
- **In-process OS sandboxing.** Real isolation is provided by running the tool in its container
  (section 9.4) rather than by reimplementing sandboxing around the smoke-test execution.
- **Non-Python targets, TUI, pasted-text/local-PDF input, numeric evaluation of generated code.**
- **Homebrew distribution.** `go install`, release binaries, and the container image (section 9)
  cover the intended audience.

## 9. Delivery and deployment

### 9.1 Build and installation

The distribution paths are `git clone` + `make build` (or `make install`, which requires
`$(go env GOPATH)/bin` on PATH), or `go install` for the no-clone case.

**Prerequisites:** Go 1.26.4 or later to build (the minimum declared in `go.mod`); Python 3.10+ on
PATH with the libraries the generated code needs (typically `torch`, `numpy`) — the smoke test is
executed with the `python3` resolved from the invoking shell's PATH, so a virtualenv providing
those libraries must be active when the tool is run; `GEMINI_API_KEY` for hosted runs; a running
Ollama server only when using `--model ollama`.

- DR1 — `make install` / `make build` from a fresh clone yields a working binary using Go modules only.
- DR2 — The README quickstart reaches a passing smoke test in five commands or fewer.
- DR3 — A missing required key exits with code 2 and a message naming the environment variable and
  suggesting `--model ollama` as the offline alternative.

### 9.2 Continuous integration

- DR4 — Every push and pull request runs format checking, `go vet`, `golangci-lint`, and
  `go test -race -cover` on Linux and macOS via GitHub Actions. Go dependencies are scanned with
  `govulncheck`. Dependencies and workflow actions are kept current by Dependabot.

### 9.3 Releases

- DR5 — Pushing a `v*` tag produces a GitHub Release containing darwin/linux × amd64/arm64
  binaries and a checksums file. The released binary's `version` command prints the release version
  (the tag without its leading `v`, injected at build time via ldflags). Each release archive is accompanied by an SBOM, and the checksums
  file is signed with cosign using the release workflow's OIDC identity (keyless; no long-lived
  signing key).

### 9.4 Container image

- DR6 — Each release publishes a multi-stage image (statically compiled Go binary on
  `python:3.12-slim`, running as a non-root user) for linux/amd64 and linux/arm64 to GHCR and to
  Docker Hub, with GHCR as the reference path. A containerized run completes a passing
  smoke test. The container is the supported way to run untrusted papers: it provides kernel-level
  isolation that the NFR1–NFR4 process-level guardrails deliberately do not. The published image is
  cosign-signed by digest and carries an SBOM covering the image's full contents (the Go binary's
  modules, the base image's packages, and any Python packages installed into the image).

## 10. Acceptance criteria

**A. Build and CLI**

- `go build ./...`, `go vet ./...`, and `go test ./...` pass.
- `--model` switches between ollama and gemini with no code change.
- `codemypaper version` prints a version; `--help` lists every section 4 flag.
- Exit codes match section 4.

**B. Behavior (end-to-end)**

- `run <id> --model ollama` completes the full generate–test–repair pipeline with no hosted calls.
- All FR1 id/URL forms resolve; extracted text is method-focused; HTML failure falls back to
  the e-print source.
- A malformed model reply triggers exactly one corrective re-prompt, not a crash (NFR6).
- On the pinned reference paper (`testdata/`), `--model gemini` produces `model.py`,
  `smoke_test.py`, and `main.py` such that `python3 smoke_test.py` exits 0, needing at most the
  single repair attempt.
- Self-correction is observable: the `--verbose` stream or `IMP_DETAILS.md` shows at least one
  failed smoke test followed by a repair that makes it pass.
- A run whose smoke test fails again after the repair attempt writes `ERROR_LOG.md` containing
  both failing outputs and exits with code 1.
- `IMP_DETAILS.md` is always written, with method, entrypoint, test verdicts, and stop reason.

**C. Guardrails and transparency**

- The path confinement (including the reserved-name check) and the output cap have unit tests
  (NFR1, NFR4), including rejection of `..` and absolute paths in model-supplied file names. The
  smoke-test timeout (NFR3) is verified through observed runs.
- Secrets never appear in logs or the `--verbose` stream.
- The README contains the success criterion and the sandbox limitation verbatim (NFR5).
- A demo recording (asciinema or GIF) shows one paper reaching a passing smoke test.

**D. Delivery**

- `make install` from a fresh clone yields a working binary (DR1).
- The README quickstart reaches a passing smoke test in five commands or fewer (DR2).
- A missing key exits 2 with an actionable message (DR3).
- CI turns red on an introduced lint or test failure and green after the fix; the README badge
  is live (DR4).
- `git tag v0.1.0` produces a Release with four binaries plus checksums, SBOMs, and a cosign
  signature on the checksums file that verifies against the workflow identity; the downloaded
  binary prints `0.1.0` (DR5).
- `docker run ghcr.io/shahriar-ferdoush/codemypaper:v0.2.0` completes a passing containerized
  run as a non-root user; `cosign verify` and `cosign verify-attestation` succeed on the published
  image digest in each registry (DR6).

**Verification commands**

```bash
go build ./... && go vet ./... && go test ./...
codemypaper version
codemypaper run 2401.XXXXX --model ollama --verbose          # offline pipeline run
GEMINI_API_KEY=… codemypaper run <pinned-id> --model gemini  # hosted run to green
( cd out/<pinned-id> && python3 smoke_test.py )              # exits 0
make install                                                 # fresh clone → working binary
docker run --rm ghcr.io/shahriar-ferdoush/codemypaper:v0.2.0 version
cosign verify --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp 'https://github.com/Shahriar-Ferdoush/codemypaper/.github/workflows/release.yml@refs/tags/.*' \
  ghcr.io/shahriar-ferdoush/codemypaper@sha256:<digest>
```
