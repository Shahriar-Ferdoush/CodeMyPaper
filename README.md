# codemypaper

`codemypaper` takes an arXiv machine-learning paper and generates a runnable Python/PyTorch
reference implementation of its core method. It is not an agent: control flow lives entirely in
Go. One model call generates the implementation, a smoke test, and an entrypoint; the tool writes
the files and runs the test itself on toy input; if the test fails, a second model call receives
the error output for exactly one repair attempt. A run that still fails ends with a structured
error log instead of further retries.

**Success criterion.** A run is successful when the generated code executes on toy input and
implements the paper's named method. Reproducing the paper's reported experimental results is
explicitly not a goal.

**Known limitation.** The smoke test is authored by the model itself. A passing run therefore
demonstrates that the generated code imports cleanly and completes a forward pass on toy input
without error; it does not independently verify fidelity to the paper's method. Final judgment of
correctness rests with a human reader of the generated source.

## Quickstart

Requires Go 1.26+ and a Python 3 environment with `torch` and `numpy` on `PATH` (a virtualenv
works: `python3 -m venv .venv && .venv/bin/pip install torch numpy`, then activate it).

```sh
git clone https://github.com/shahriar-ferdoush/codemypaper && cd codemypaper
make install                     # installs to $(go env GOPATH)/bin — make sure it's on PATH
export GEMINI_API_KEY=...        # free tier works
codemypaper run 1706.03762
(cd out/1706.03762 && python3 smoke_test.py)   # exits 0
```

To run fully offline instead, use the local backend: start Ollama (`ollama serve`), pull a model
(`ollama pull qwen2.5-coder:3b`), and pass `--model ollama`. No API key needed.

## Usage

```
codemypaper run <arxiv-id-or-url> [flags]
codemypaper version
```

The input can be a bare id (`2401.01234`), an `abs/` or `pdf/` URL, with or without a version
suffix.

| Flag | Default | Meaning |
|---|---|---|
| `--model` | `gemini` | chat backend: `gemini` or `ollama` |
| `--gemini-model` | `gemini-2.5-flash` | hosted model id |
| `--ollama-model` | `qwen2.5-coder:3b` | local model id |
| `--out` | `./out/<id>` | output directory |
| `--timeout` | `2m` | smoke-test timeout |
| `--max-context-chars` | `60000` | paper context budget |
| `--refetch` | off | bypass the cached paper source and fetch fresh |
| `--verbose` | off | stream debug logging to stderr |

Configuration comes from the environment: `GEMINI_API_KEY` for the hosted backend, `OLLAMA_HOST`
(default `http://localhost:11434`) for the local one. Secrets are never accepted as flags and
never logged.

Exit codes are part of the contract, so runs are scriptable:

| Code | Meaning |
|---|---|
| 0 | smoke test passed |
| 1 | run ended without a green smoke test (`ERROR_LOG.md` written) |
| 2 | usage or configuration error (e.g. missing API key) |
| 3 | fatal error (paper fetch or backend unreachable) |

## What a run produces

Every run, successful or not, leaves an inspectable directory:

```
out/<id>/
├── model.py          the method implementation
├── smoke_test.py     toy-input test the tool ran
├── main.py           minimal entrypoint
├── IMP_DETAILS.md    run summary: paper, backend, method, verdicts, stop reason
├── run.log           full run log
├── paper.html        the fetched paper source (or paper.tar.gz / paper.tex.gz)
├── paper.meta.json   cache sidecar: which source won, title, abstract
└── ERROR_LOG.md      only on failure: both failing outputs and what the repair changed
```

Rerunning the same id reuses the cached paper source with no network calls; `--refetch` forces a
fresh fetch (needed if the paper was revised on arXiv, since the cache is keyed on the bare id).

## How it works

The pipeline is a fixed sequence, not a loop: the model is called at most twice per run, and Go
decides every step. Call #1 must return all files in a delimited file-block format in one reply;
Go parses and validates the reply, writes the files, and runs `python3 smoke_test.py` under a
timeout. On failure, call #2 continues the same conversation with the failing output and may only
rewrite existing files. A malformed reply gets exactly one corrective re-prompt per call.

Paper ingestion tries sources in order — `arxiv.org/html`, the ar5iv mirror, then the LaTeX
e-print tarball — and keeps the method-bearing sections within the context budget. PDF parsing is
deliberately excluded: for arXiv papers the LaTeXML source is available directly and preserves the
equations that carry the method, where PDF text extraction mangles them.

## Development

The Makefile wraps the standard Go workflow and stamps the build with a version from
`git describe`, so `codemypaper version` reports the tag or commit it was built from:

| Target | What it does |
|---|---|
| `make build` | build `bin/codemypaper` with the version stamp (default target) |
| `make install` | install the stamped binary via `go install` |
| `make test` | `go test ./...` |
| `make lint` | `go vet`, plus `golangci-lint` when installed |
| `make fmt` / `make fmt-check` | format the tree / fail if anything is unformatted |
| `make clean` | remove `bin/` |

Plain `go build ./...` works too — you just get a binary whose `version` prints `dev`.

## Security model

The guardrails are process-level controls, **not a sandbox**: generated code runs as the invoking
user, so run only papers you trust. What the tool does enforce: the only command it ever executes
is `python3 smoke_test.py` (the model never chooses a command), model-supplied file paths are
confined to the output directory and cannot overwrite pipeline-owned files, and the test run has a
timeout and an output cap. None of that constrains what the generated Python does once it runs.
No container image ships yet; if you need real isolation, run the tool inside a container or VM of
your own.
