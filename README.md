# CodeMyPaper

`codemypaper` takes an arXiv machine-learning paper and generates a runnable Python/PyTorch
reference implementation of its core method. It is not an agent: control flow lives entirely in Go.
One model call generates the implementation, a smoke test, and an entrypoint; the tool writes the
files and runs the test itself on toy input; if the test fails, a second call receives the error
output for exactly one repair attempt. A run that still fails ends with a structured error log
instead of further retries.

**Success criterion.** A run succeeds when the generated code executes on toy input and implements
the paper's named method. Reproducing the paper's reported results is explicitly not a goal.

**Known limitation.** The smoke test is authored by the model itself. A passing run demonstrates
that the generated code imports cleanly and completes a forward pass on toy input — not fidelity to
the paper's method. Final judgment of correctness rests with a human reader of the generated source.

## Quickstart

The image carries the Python runtime the generated code needs, so a containerized run needs nothing
but Docker on the host.

```sh
docker pull ghcr.io/shahriar-ferdoush/codemypaper:v0.2.0
export GEMINI_API_KEY=...        # free tier works
docker run --rm -e GEMINI_API_KEY -v "$PWD/docker_out:/work/out" \
  ghcr.io/shahriar-ferdoush/codemypaper:v0.2.0 run 1706.03762
```

Output lands in `docker_out/1706.03762/`. The container runs as a non-root user, and the generated
code it executes can reach only that mounted directory.

The same image also publishes to Docker Hub, where no registry prefix is needed:

```sh
docker pull shahriarferdoush/codemypaper:v0.2.0
```

Both registries carry linux/amd64 and linux/arm64; `docker pull` selects for your machine. Semver
tags are immutable pins of a release; `:latest` is a moving alias that follows the newest stable
release and never points at a pre-release.

`docker_out/` is kept separate from a native run's `out/` so the two are never confused. The cost:
they don't share the cached paper source, so the first container run of a paper fetches it again.

## Install a release binary

Each [release](https://github.com/shahriar-ferdoush/codemypaper/releases) ships prebuilt archives
for macOS and Linux (amd64/arm64), named `codemypaper_<version>_<os>_<arch>.tar.gz`.

```sh
tar -xzf codemypaper_*.tar.gz
./codemypaper version
```

Unlike the image, a bare binary needs Python 3 with `torch` and `numpy` on the host — `codemypaper`
runs the smoke test with the `python3` found on your `PATH`. With a virtualenv
(`python3 -m venv .venv && .venv/bin/pip install torch numpy`), it must be **active in the shell
where you run `codemypaper`**, not just when rerunning the test yourself.

## Usage

```
codemypaper run <arxiv-id-or-url> [flags]
codemypaper version
```

The input can be a bare id (`2401.01234`), an `abs/` or `pdf/` URL, with or without a version suffix.

| Flag | Default | Meaning |
|---|---|---|
| `--model` | `gemini` | chat backend: `gemini` or `ollama` |
| `--gemini-model` | `gemini-3.1-flash-lite` | hosted model id |
| `--ollama-model` | `qwen2.5-coder:3b` | local model id |
| `--out` | `./out/<id>` | output directory |
| `--timeout` | `2m` | smoke-test timeout |
| `--max-context-chars` | `60000` | paper context budget |
| `--refetch` | off | bypass the cached paper source and fetch fresh |
| `--verbose` | off | stream debug logging to stderr |

Configuration comes from the environment: `GEMINI_API_KEY` for the hosted backend, `OLLAMA_HOST`
(default `http://localhost:11434`) for the local one. Secrets are never accepted as flags and never
logged.

Exit codes are part of the contract, so runs are scriptable:

| Code | Meaning |
|---|---|
| 0 | smoke test passed |
| 1 | run ended without a green smoke test (`ERROR_LOG.md` written) |
| 2 | usage or configuration error (e.g. missing API key) |
| 3 | fatal error (paper fetch or backend unreachable) |

### Running offline

Start Ollama (`ollama serve`), pull a model (`ollama pull qwen2.5-coder:3b`), pass `--model ollama`.
No API key needed.

Ollama is deliberately not bundled in the image — it would triple the size for a backend most runs
don't use. To reach a host Ollama from a container:

```sh
docker run --rm -e OLLAMA_HOST=http://host.docker.internal:11434 \
  -v "$PWD/docker_out:/work/out" \
  ghcr.io/shahriar-ferdoush/codemypaper:v0.2.0 run 2401.12345 --model ollama
```

On Linux, add `--add-host=host.docker.internal:host-gateway` and bind the server to `0.0.0.0`. Note
`OLLAMA_HOST` means two different things: a bind address to the Ollama server, a dial URL to this
client.

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

## Verifying a download

Both the archives and the image are signed in CI with keyless [cosign](https://docs.sigstore.dev).
The signature binds the artifact to this repository's release workflow identity — so the
`--certificate-identity-regexp` is not optional: without it a passing check only proves that
*somebody* signed it.

**Release archives.** Check the hash, then verify the checksums file that vouches for it:

```sh
shasum -a 256 --check --ignore-missing checksums.txt

cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp 'https://github.com/Shahriar-Ferdoush/codemypaper/\.github/workflows/release\.yml@refs/tags/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

A verified checksums file plus a matching hash proves the archive came from this repo's CI. Each
archive also has a `<archive>.sbom.json` sidecar listing the module versions compiled into it.

**The image.** Signed by digest, and carrying an SBOM attestation. There is no `.bundle` to
download — for images the signature is stored in the registry next to the image itself:

```sh
IMG=ghcr.io/shahriar-ferdoush/codemypaper
DIGEST=$(docker buildx imagetools inspect --format '{{.Manifest.Digest}}' $IMG:v0.2.0)
ID='https://github.com/Shahriar-Ferdoush/codemypaper/\.github/workflows/release\.yml@refs/tags/.*'
ISS=https://token.actions.githubusercontent.com

cosign verify             --certificate-identity-regexp "$ID" --certificate-oidc-issuer "$ISS" "$IMG@$DIGEST"
cosign verify-attestation --certificate-identity-regexp "$ID" --certificate-oidc-issuer "$ISS" \
  --type spdxjson "$IMG@$DIGEST"
```

Substitute `shahriarferdoush/codemypaper` for the Docker Hub copy, which is signed separately and
has its own digest.

The binary inside the image is compiled during the image build, so it is a different artifact from
the release archives — same source and same version string, different bytes and different hashes.
Verify each against its own signature.

## How it works

The pipeline is a fixed sequence, not a loop: the model is called at most twice per run, and Go
decides every step. Call #1 must return all files in a delimited file-block format in one reply; Go
parses and validates the reply, writes the files, and runs `python3 smoke_test.py` under a timeout.
On failure, call #2 continues the same conversation with the failing output and may only rewrite
existing files. A malformed reply gets exactly one corrective re-prompt per call.

Paper ingestion tries sources in order — `arxiv.org/html`, the ar5iv mirror, then the LaTeX e-print
tarball — and keeps the method-bearing sections within the context budget. PDF parsing is
deliberately excluded: for arXiv papers the LaTeXML source is available directly and preserves the
equations that carry the method, where PDF text extraction mangles them.

## Building from source

Requires Go 1.26+, plus the Python environment described under [Install a release
binary](#install-a-release-binary).

```sh
git clone https://github.com/shahriar-ferdoush/codemypaper && cd codemypaper
make                             # builds ./bin/codemypaper
export GEMINI_API_KEY=...
./bin/codemypaper run 1706.03762
(cd out/1706.03762 && python3 smoke_test.py)   # exits 0
```

`make install` puts the binary in `$(go env GOPATH)/bin` (usually `~/go/bin`) for a bare
`codemypaper` command. That directory is **not on PATH by default** — if the command isn't found:

```sh
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc   # bash: ~/.bashrc
```

The Makefile stamps the build with a version from `git describe`, so `codemypaper version` reports
the tag or commit it was built from. Plain `go build ./...` works too — you just get a binary whose
`version` prints `dev`.

| Target | What it does |
|---|---|
| `make build` | build `bin/codemypaper` with the version stamp (default target) |
| `make install` | install the stamped binary to `$(go env GOPATH)/bin` |
| `make test` | `go test ./...` |
| `make lint` | `go vet`, plus `golangci-lint` when installed |
| `make fmt` / `make fmt-check` | format the tree / fail if anything is unformatted |
| `make clean` | remove `bin/` |
| `make docker-build` | build the image locally as `codemypaper:latest` |
| `make docker-run` | run it with `docker_out/` mounted — `ARGS="run 2401.12345 --verbose"` |
| `make docker-shell` | shell into the image with the same mount |

## Security model

The guardrails are process-level controls, **not a sandbox**: generated code runs as the invoking
user, so run only papers you trust. What the tool does enforce: the only command it ever executes is
`python3 smoke_test.py` (the model never chooses a command), model-supplied file paths are confined
to the output directory and cannot overwrite pipeline-owned files, and the test run has a timeout
and an output cap. None of that constrains what the generated Python does once it runs.

For real isolation, use the image: generated code then executes as a non-root user inside the
container's mount, PID, and user namespaces, and the only host path it can reach is the `out/`
directory you bind-mount. Be precise about what that does *not* cover — a run fetches from arXiv and
calls the model API, so it needs network egress; the container isolates the filesystem and process
view, not the network.
