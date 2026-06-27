# codemypaper — Build Plan (step by step)

> A hands-on path to a working tool, in the order you should type things. Each part ends with a **Checkpoint**
> you can run. Code blocks are starter code — type them (don't just paste; you're learning Go). The project
> lives in `/Users/shahriar/Documents/Personal/CodeMyPaper/` (a subfolder of your personal-code dir; you'll
> push only `CodeMyPaper`) under the local module name `codemypaper`.
> **Go is already installed (`go1.26.4`) — skip Part 0's install; it's kept only for the uninstall in Part 8.**
>
> Pairs with [`PROJECT_SPECIFICATION.md`](./PROJECT_SPECIFICATION.md) (the contract) and
> [`DESIGN.md`](./DESIGN.md) (the architecture). Milestones M1–M4 here match DESIGN §9.
>
> **The inner dev loop you'll use constantly:**
> `go run ./cmd/codemypaper <args>` (run) · `go build ./...` (compile-check) · `go vet ./...` (lint) ·
> `gofmt -w .` (format) · `go test ./...` (tests).

---

## Part 0 — Install Go on macOS

You're on macOS with no Go. Pick **one** install method.

### 0.1 Install — option A: Homebrew (recommended; trivial uninstall)
```bash
# If you don't have Homebrew: https://brew.sh
brew install go
```

### 0.1 Install — option B: official package
Download the macOS installer from https://go.dev/dl/ (pick **Apple silicon / arm64** for M1/M2/M3 Macs,
**Intel / amd64** otherwise), open the `.pkg`, follow the installer. It installs to `/usr/local/go` and adds
it to PATH.

### 0.2 Verify
```bash
go version          # e.g. go version go1.22.x darwin/arm64
go env GOPATH GOBIN # GOPATH defaults to ~/go ; installed binaries land in ~/go/bin
```
Make sure `~/go/bin` is on your PATH (so `go install`-ed tools run by name). Add to `~/.zshrc` if needed:
```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

### 0.3 Editor
VS Code + the official **Go** extension (installs `gopls`, the language server) gives autocomplete, jump-to-def,
and inline errors — hugely helpful while learning. On first open of a `.go` file it'll offer to install tools;
accept.

### 0.4 Dev dependency: Ollama (free local model for M1–M2)
You already have Ollama — just make sure the server is running and the dev model is pulled:
```bash
ollama serve &                     # starts the local server on :11434 (skip if already running)
ollama pull qwen2.5-coder:3b       # a small coder model; ~2 GB
```
(Python + a virtualenv with torch/numpy are only needed once generated smoke-tests run — see Part 6 step 7.)

> **Note for later:** everything Go put on your machine — the toolchain, `~/go` (modules + installed binaries),
> and the build cache — is removed in **Part 8**. Skip there when the project's done.

---

## Part 1 — Bootstrap the project

### Step 1.1 — Create the module
Create the project folder and initialize the module inside it. We use a short local module name `codemypaper`
(no GitHub username needed); if you later publish, repoint it with `go mod edit -module
github.com/<you>/codemypaper` and update imports.
```bash
mkdir -p /Users/shahriar/Documents/Personal/CodeMyPaper
cd /Users/shahriar/Documents/Personal/CodeMyPaper
go mod init codemypaper                        # creates go.mod
git init                                        # this is the repo you'll push
mkdir -p cmd/codemypaper internal/llm internal/tools internal/agent internal/arxiv internal/vision testdata
```

### Step 1.2 — First runnable program
`cmd/codemypaper/main.go`:
```go
package main

import "fmt"

func main() {
	fmt.Println("codemypaper: hello")
}
```

### Checkpoint 1
```bash
go run ./cmd/codemypaper      # prints: codemypaper: hello
```
You now have a compiling Go module. Commit it.

---

## Part 2 — M1: CLI + `LLMClient` + Ollama

**Goal:** `codemypaper run <id>` sends one chat to local Ollama and prints the reply.
**Go concepts:** packages, structs, **interfaces**, `net/http`, JSON (un)marshalling, error handling.

### Step 2.1 — Add Cobra (CLI framework)
```bash
go get github.com/spf13/cobra@latest
```

### Step 2.2 — The `llm` package: the key interface
`internal/llm/client.go`:
```go
package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

// LLMClient is the single seam between the agent and any chat backend.
// Anything with these two methods *is* an LLMClient — that's Go's implicit interfaces.
type LLMClient interface {
	Chat(ctx context.Context, messages []Message) (string, error)
	Name() string
}
```

### Step 2.3 — The Ollama implementation
`internal/llm/ollama.go`:
```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type Ollama struct {
	Host  string
	Model string
}

func NewOllama(model string) *Ollama {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}
	return &Ollama{Host: host, Model: model}
}

func (o *Ollama) Name() string { return "ollama:" + o.Model }

// wire types: how Ollama's /api/chat wants the JSON.
type ollamaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ollamaReq struct {
	Model    string      `json:"model"`
	Messages []ollamaMsg `json:"messages"`
	Stream   bool        `json:"stream"`
}
type ollamaResp struct {
	Message ollamaMsg `json:"message"`
}

func (o *Ollama) Chat(ctx context.Context, messages []Message) (string, error) {
	msgs := make([]ollamaMsg, len(messages))
	for i, m := range messages {
		msgs[i] = ollamaMsg{Role: string(m.Role), Content: m.Content}
	}
	body, err := json.Marshal(ollamaReq{Model: o.Model, Messages: msgs, Stream: false})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama unreachable (is `ollama serve` running?): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}
	var out ollamaResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Message.Content, nil
}
```

### Step 2.4 — Wire the CLI
`cmd/codemypaper/main.go` (replace the hello version):
```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"codemypaper/internal/llm"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "codemypaper",
		Short: "Turn an arXiv paper into a runnable reference implementation",
	}
	root.AddCommand(versionCmd(), runCmd())
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Println(version) },
	}
}

func runCmd() *cobra.Command {
	var ollamaModel string
	cmd := &cobra.Command{
		Use:   "run <arxiv-id>",
		Short: "Generate an implementation for a paper",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := llm.NewOllama(ollamaModel)
			fmt.Println("backend:", client.Name())
			reply, err := client.Chat(cmd.Context(), []llm.Message{
				{Role: llm.RoleUser, Content: "Reply with one short sentence to confirm you are working."},
			})
			if err != nil {
				return err
			}
			fmt.Println("model says:", reply)
			return nil
		},
	}
	cmd.Flags().StringVar(&ollamaModel, "ollama-model", "qwen2.5-coder:3b", "local model id")
	return cmd
}
```

### Checkpoint 2 (M1 done)
```bash
go mod tidy                              # resolves the cobra dependency in go.mod/go.sum
ollama serve &                           # if not already running
go run ./cmd/codemypaper run 2401.00001  # the id is ignored for now; you should see a model reply
```
✅ You have a swappable backend behind an interface and a real chat round-trip.

---

## Part 3 — M2: Tools + registry + the agent loop

**Goal:** the model can `write_file` and `run_command` via the JSON protocol, and the loop runs act→observe.
**Go concepts:** interfaces + a registry map, `os/exec`, `context` timeouts, regex/JSON parsing.

### Step 3.1 — Tool interface + registry
`internal/tools/tools.go`:
```go
package tools

import (
	"context"
	"fmt"
	"strings"
)

type Result struct {
	Output   string
	IsError  bool
	ExitCode int
}

type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, args map[string]any) (Result, error)
}

type Registry struct{ tools map[string]Tool }

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(t Tool) { r.tools[t.Name()] = t }

func (r *Registry) Run(ctx context.Context, name string, args map[string]any) (Result, error) {
	t, ok := r.tools[name]
	if !ok {
		return Result{IsError: true, Output: "unknown tool: " + name}, nil
	}
	return t.Run(ctx, args)
}

func (r *Registry) Descriptions() string {
	var b strings.Builder
	for _, t := range r.tools {
		fmt.Fprintf(&b, "- %s: %s\n", t.Name(), t.Description())
	}
	return b.String()
}
```

### Step 3.2 — The cwd-jail helper (NFR1)
`internal/tools/jail.go`:
```go
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeJoin keeps file/command access inside base; rejects ".." and absolute paths.
func safeJoin(base, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths not allowed: %s", rel)
	}
	full, err := filepath.Abs(filepath.Join(base, rel))
	if err != nil {
		return "", err
	}
	rbase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	if full != rbase && !strings.HasPrefix(full, rbase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes project dir: %s", rel)
	}
	return full, nil
}

func capOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…[truncated]"
}
```

### Step 3.3 — `write_file` and `read_file`
`internal/tools/files.go`:
```go
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type WriteFile struct{ BaseDir string }

func (w WriteFile) Name() string        { return "write_file" }
func (w WriteFile) Description() string { return "write_file(path, content): create/overwrite a file in the project dir" }

func (w WriteFile) Run(_ context.Context, args map[string]any) (Result, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	full, err := safeJoin(w.BaseDir, path)
	if err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	return Result{Output: fmt.Sprintf("wrote %d bytes to %s", len(content), path)}, nil
}

type ReadFile struct{ BaseDir string }

func (r ReadFile) Name() string        { return "read_file" }
func (r ReadFile) Description() string { return "read_file(path): read a file in the project dir" }

func (r ReadFile) Run(_ context.Context, args map[string]any) (Result, error) {
	path, _ := args["path"].(string)
	full, err := safeJoin(r.BaseDir, path)
	if err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return Result{IsError: true, Output: err.Error()}, nil
	}
	return Result{Output: capOutput(string(b), 20000)}, nil
}
```

### Step 3.4 — `run_command` (allowlist + timeout + cwd-jail; NFR2–4)
`internal/tools/runcmd.go`:
```go
package tools

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type RunCommand struct {
	BaseDir string
	Timeout time.Duration
}

var allowed = map[string]bool{
	"python": true, "python3": true, "pip": true, "pip3": true,
	"ls": true, "cat": true, "pytest": true,
}

func (r RunCommand) Name() string        { return "run_command" }
func (r RunCommand) Description() string { return "run_command(cmd): run an allowlisted command in the project dir" }

func (r RunCommand) Run(ctx context.Context, args map[string]any) (Result, error) {
	cmdStr, _ := args["cmd"].(string)
	fields := strings.Fields(cmdStr) // NOTE: naive split (no quotes) — fine for v1
	if len(fields) == 0 {
		return Result{IsError: true, Output: "empty command"}, nil
	}
	if !allowed[fields[0]] {
		return Result{IsError: true, Output: "command not allowed: " + fields[0]}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	c := exec.CommandContext(ctx, fields[0], fields[1:]...)
	c.Dir = r.BaseDir
	out, err := c.CombinedOutput()
	output := capOutput(string(out), 20000)

	if ctx.Err() == context.DeadlineExceeded {
		return Result{IsError: true, Output: output + "\n[timed out]", ExitCode: -1}, nil
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return Result{Output: output, ExitCode: ee.ExitCode()}, nil // program ran, returned nonzero
		}
		return Result{IsError: true, Output: err.Error()}, nil // couldn't start
	}
	return Result{Output: output, ExitCode: 0}, nil
}
```

### Step 3.5 — The tool-call parser (protocol from DESIGN §5)
`internal/agent/parse.go`:
```go
package agent

import (
	"encoding/json"
	"errors"
	"regexp"
)

type toolCall struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

var jsonBlock = regexp.MustCompile("(?s)```json\\s*(.*?)```")

// parseToolCall extracts the LAST ```json {...}``` block and unmarshals it.
func parseToolCall(raw string) (toolCall, error) {
	m := jsonBlock.FindAllStringSubmatch(raw, -1)
	if len(m) == 0 {
		return toolCall{}, errors.New("no ```json tool block found")
	}
	var tc toolCall
	if err := json.Unmarshal([]byte(m[len(m)-1][1]), &tc); err != nil {
		return toolCall{}, err
	}
	if tc.Tool == "" {
		return toolCall{}, errors.New(`missing "tool" field`)
	}
	return tc, nil
}
```

### Step 3.6 — A minimal loop (paperless for now)
`internal/agent/loop.go`:
```go
package agent

import (
	"context"
	"fmt"

	"codemypaper/internal/llm"
	"codemypaper/internal/tools"
)

type Agent struct {
	LLM      llm.LLMClient
	Tools    *tools.Registry
	MaxIters int
	Verbose  bool
}

func (a *Agent) Run(ctx context.Context, systemPrompt, task string) error {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: task},
	}
	for i := 1; i <= a.MaxIters; i++ {
		raw, err := a.LLM.Chat(ctx, msgs)
		if err != nil {
			return err
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: raw})
		if a.Verbose {
			fmt.Printf("\n--- iter %d ---\n%s\n", i, raw)
		}

		call, perr := parseToolCall(raw)
		if perr != nil { // malformed → one corrective re-prompt (NFR6)
			msgs = append(msgs, llm.Message{Role: llm.RoleUser,
				Content: "Your reply had no valid tool call. Reply with exactly one ```json {\"tool\":...,\"args\":...}``` block."})
			continue
		}
		if call.Tool == "finish" {
			fmt.Println("\n✅ finished:", call.Args["summary"])
			return nil
		}
		res, _ := a.Tools.Run(ctx, call.Tool, call.Args)
		obs := fmt.Sprintf("Observation from %s (exit=%d, error=%v):\n%s", call.Tool, res.ExitCode, res.IsError, res.Output)
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: obs})
	}
	fmt.Println("\n⏹ stopped: max iterations reached")
	return nil
}
```

### Step 3.7 — Hook it into `run` and add a temporary system prompt
In `runCmd`, after building the client, register tools and run a tiny task so you can watch the loop work
(replace the one-shot Chat from Step 2.4):
```go
out := "./out/scratch"
reg := tools.NewRegistry()
reg.Register(tools.WriteFile{BaseDir: out})
reg.Register(tools.ReadFile{BaseDir: out})
reg.Register(tools.RunCommand{BaseDir: out, Timeout: 120 * time.Second})

ag := &agent.Agent{LLM: client, Tools: reg, MaxIters: 6, Verbose: true}
sys := "You can call tools by emitting one ```json block: " +
	"{\"tool\":\"write_file\",\"args\":{\"path\":\"hello.py\",\"content\":\"print('hi')\"}}. " +
	"Tools:\n" + reg.Descriptions() +
	"When done, emit {\"tool\":\"finish\",\"args\":{\"summary\":\"...\"}}."
return ag.Run(cmd.Context(), sys, "Write hello.py that prints hi, run it with `python3 hello.py`, then finish.")
```
(You'll need `os.MkdirAll(out, 0o755)` before running, and imports for `time`, `os`, the `agent` and `tools`
packages.)

### Checkpoint 3 (M2 done)
```bash
go build ./... && go run ./cmd/codemypaper run 2401.00001 --ollama-model qwen2.5-coder:3b
```
Watch it write `out/scratch/hello.py`, run it, observe output, and finish. The 3B model is dumb — if it
fumbles the protocol, that's expected; you're testing the *machinery*, not the intelligence.
✅ Core agent loop with real tools and self-observation.

---

## Part 4 — M3: arXiv ingestion + Gemini + the real prompt

**Goal:** feed a real paper's method text to a capable hosted model.
**Go concepts:** `context` deadlines, HTTP clients against real APIs, heavier string handling.

Steps (write these yourself, guided):
1. **`internal/arxiv/parse.go` — `ParseID`.** Normalize the FR1 forms with `strings`/`regexp` to a bare id
   like `2401.01234`. Unit-test it first (`parse_test.go`) — a great first Go test.
2. **`internal/arxiv/fetch.go` — `Fetch`.** GET `https://ar5iv.org/html/<id>`; on non-200 fall back to the
   e-print tarball. Strip HTML to text (start crude: regex out `<script>/<style>`, then tags; refine later).
3. **`internal/arxiv/paper.go` — types + `PromptText`.** Split into sections by heading; flag
   `MethodRelevant` on the §DESIGN keyword list; assemble title+abstract+method sections, trimmed to
   `maxChars`, set `Truncated`. (Figures come in M3.5 — leave `Figures`/`FigureNotes` empty for now.)
4. **`internal/llm/gemini.go` — `Gemini` implementing `LLMClient`.** POST to the Generative Language API
   `…/models/<model>:generateContent?key=<GEMINI_API_KEY>`. Gemini has no system role → prepend the system
   text to the first user turn. Same wire-types pattern as Ollama. Return `ErrNoAPIKey` when the env var is
   empty (drives exit code `2`).
   ```go
   // request shape (sketch)
   type geminiReq struct {
       Contents []geminiContent `json:"contents"`
   }
   type geminiContent struct {
       Role  string       `json:"role"`  // "user" | "model"
       Parts []geminiPart `json:"parts"`
   }
   type geminiPart struct{ Text string `json:"text"` }
   // response: candidates[0].content.parts[0].text
   ```
5. **Backend selection in `main.go`.** Add `--model`, `--gemini-model`, `--out`, `--max-iters`, `--timeout`,
   `--max-context-chars`, `--verbose`. Build `client` from `--model` (gemini default; ollama path unchanged).
6. **Real prompt in `internal/agent/prompt.go`** per DESIGN §6: role, success bar, tool catalog + protocol
   example, hard rules, then `paper.PromptText()`. Set the first user message to the implement-the-method task.
7. Default `--out` to `./out/<id>` and `os.MkdirAll` it.

### Checkpoint 4 (M3 done)
```bash
export GEMINI_API_KEY=…   # free tier
source .venv/bin/activate           # so the agent's smoke-tests run in the venv (Part 6 step 7)
go run ./cmd/codemypaper run <id-of-a-simple-method-paper> --model gemini --verbose
```
You should see the model write `model.py` + `smoke_test.py` grounded in the real paper text.
✅ A grounded agent producing paper-specific code.

---

## Part 5 — M3.5 (optional): vision pre-pass

**Goal:** describe architecture/pipeline figures into the prompt. Build only after M3 is solid.
Steps:
1. **`internal/arxiv/figures.go`.** Parse ar5iv `<figure>`/`<figcaption>`; fill `Figures` with refs/captions/
   image URLs; flag `MethodRelevant` on diagram keywords. Add `Figure.FetchImage` (GET → bytes + MIME).
2. **`internal/llm/vision.go`.** Add the `Image` type and `VLMClient` interface (parallel to `LLMClient`).
3. **`internal/llm/gemini_vision.go`** (and optionally `groq_vision.go`). Implement `Describe` — Gemini takes
   `inline_data` parts; Groq takes OpenAI-style `image_url` data-URIs.
4. **`internal/vision/describe.go` — `Annotate`.** No-op if `vlm == nil`. Pick top `--max-figures`
   method-relevant figures, call `Describe` with the DESIGN §3.4 instruction, fill `FigureNotes`, write
   `figure_notes.md`, and cache by `sha256(image+instruction)`.
5. **Wire `--vision auto|gemini|groq|none`** (auto rules in spec §4) in `main.go`; pass the resolved `VLMClient`
   (or nil) into the ingestion step before building the prompt.

### Checkpoint 5
```bash
go run ./cmd/codemypaper run <id-with-a-diagram> --vision gemini --verbose   # writes figure_notes.md
go run ./cmd/codemypaper run <same-id> --vision none                          # identical text-only path
```
✅ Optional figure grounding, fully degradable.

---

## Part 6 — M4: self-correction, packaging, polish

**Goal:** the loop reliably drives a smoke-test to green; the project is installable and documented.
Steps:
1. **`finish` tool** (`internal/tools/finish.go`) carrying `summary`/`method`/`entrypoint`; have the loop
   capture these into an `Outcome` (DESIGN §3.5) and add `--wall-budget` + a `StopReason`.
2. **`RUN_SUMMARY.md` writer** (always) and `transcript.jsonl` (when `--verbose`) per spec §7.
3. **Tighten self-correction:** make the prompt insist on running `smoke_test.py` and only calling `finish`
   after exit 0; confirm a failed run → fix → green shows in the transcript.
4. **Exit codes** (spec §4) from `main.go` based on the `Outcome`.
5. **Tests** for the guardrails (jail rejects `..`/absolute; allowlist; timeout; output cap) and `ParseID`.
6. **`Makefile`** (DESIGN §8) and **`README.md`** with the verbatim success bar, the "not a sandbox" note, and
   the ≤5-command Quickstart. Record a short asciinema/GIF.
7. **Python venv for real smoke-tests.** Create an isolated environment so generated tests run without
   touching system Python, then run codemypaper with it **active** — the agent's `run_command` calls
   `python3`, which resolves to the venv while it's activated:
   ```bash
   python3 -m venv .venv
   source .venv/bin/activate
   pip install --upgrade pip
   pip install torch numpy        # CPU build; add more as papers require
   ```
   Keep `.venv/` in `.gitignore`. Add packages here whenever a generated `requirements.txt` needs them.

### Checkpoint 6 (M4 / done)
Run the full **Verification commands** block in spec §9 and tick acceptance groups A–D.
✅ Shippable v1.

---

## Part 7 — Plan ↔ acceptance map (quick reference)

| Build part | Satisfies (spec §9) |
|---|---|
| M1 (Part 2) | A: `--model` switch, version/help, build |
| M2 (Part 3) | B: ollama loop, malformed re-prompt; C: guardrail tests |
| M3 (Part 4) | B: FR1 forms, method text, ar5iv→e-print fallback, gemini green run |
| M3.5 (Part 5) | B′: vision on/off parity, figure_notes, caching, graceful degrade |
| M4 (Part 6) | B: RUN_SUMMARY, self-correction; C: honesty/demo; D: `make install`, Quickstart |

---

## Part 8 — Clean uninstall of Go (after the project)

Do this only when you're finished and want Go gone.

```bash
# 1) Wipe Go's caches first (module cache is read-only; this avoids permission errors)
go clean -cache -modcache

# 2) Remove the toolchain
brew uninstall go                        # if you installed via Homebrew
#   --- OR, if you used the official .pkg installer: ---
sudo rm -rf /usr/local/go
sudo rm -f /etc/paths.d/go               # only exists with the pkg installer

# 3) Remove GOPATH (downloaded modules + installed binaries, incl. codemypaper) and build cache
rm -rf ~/go
rm -rf ~/Library/Caches/go-build

# 4) Remove the PATH line you added in Step 0.2 from ~/.zshrc (edit the file), then:
source ~/.zshrc

# verify it's gone
go version    # should say: command not found
```
Your project in `/Users/shahriar/Documents/Personal/CodeMyPaper` is untouched by the Go uninstall — delete
that folder manually if you also want it gone.
