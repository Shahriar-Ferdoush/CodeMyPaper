# Go Notes — codemypaper · Part 3 (M2: tools, registry, the agent loop)

What I learned wiring the act→observe→recover loop with local Ollama. Earlier parts (modules, packages, interfaces, Ollama HTTP, Cobra) are in the git history of this file.

---

## 1. Maps — Go's `dict`, typed and with sharp edges

```go
type Registry struct{ tools map[string]Tool }
func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }
```

- **`map[string]Tool`** — keys `string`, values the `Tool` *interface*. Every value is guaranteed at compile time to have the interface's methods. Python's `dict` without the runtime surprises.
- **A `nil` map reads fine but PANICS on write.** `var m map[string]Tool` then `m["x"]=t` crashes (`assignment to entry in nil map`). Always initialize — `map[string]Tool{}` or `make(map[string]Tool)`. That's the only reason `NewRegistry` exists.
- **Iteration order is randomized on purpose** (`for k, v := range m`). Go shuffles it so you can't lean on insertion order. Sort the keys first if stable output matters.
- `delete(m, key)` to remove; `len(m)` for size.

| Python | Go |
|---|---|
| `d = {}` | `m := map[K]V{}` / `make(map[K]V)` |
| `d[k]` (KeyError if absent) | `m[k]` → **zero value** if absent (never errors) |
| `k in d` | `_, ok := m[k]` |
| `del d[k]` | `delete(m, k)` |
| insertion-ordered | **randomized** iteration |

## 2. The comma-ok idiom — the heart of dispatch

```go
func (r *Registry) Run(ctx context.Context, name string, args map[string]any) (Result, error) {
	t, ok := r.tools[name]   // map lookup returns (value, present?)
	if !ok {
		return Result{IsError: true, Output: "unknown tool: " + name}, nil
	}
	return t.Run(ctx, args)  // dynamic dispatch on the concrete tool
}
```

- A map lookup **never fails** — a missing key returns the zero value, not a KeyError. The optional second boolean **`value, ok := m[key]`** tells you whether the key was actually there. Same idiom shows up in type assertions (`v, ok := x.(T)`) and channel receives.
- So an unknown tool name from a dumb model becomes a clean `Result{IsError:true}` to feed back — never a `nil`-deref panic.
- `t.Run(...)` is **dynamic dispatch**: one `map[string]Tool` holds many concrete types, Go reads the value's type to call the right `Run` — no `switch name {...}` ladder.

## 3. The registry pattern (why interfaces pay off)

`WriteFile`, `ReadFile`, `RunCommand`, `finish` all satisfy one `Tool` interface, so they're interchangeable values in one map. Adding a capability = write one struct + `reg.Register(...)`. **The loop never changes.** Open for extension, closed for modification.

```go
reg.Register(tools.WriteFile{BaseDir: out})   // different concrete types...
reg.Register(tools.RunCommand{BaseDir: out})  // ...all are Tools, so all fit.
```

Wrapping the map in a `struct` (not a bare map) buys: the `tools` field is **lowercase → unexported** so outsiders can't corrupt it or skip `Register`; methods give a clean API + a home for future locking/logging. Use a **`*Registry` pointer receiver** so all callers share one map (a value receiver copies the struct and you'd register into a throwaway).

## 4. `strings.Builder` — efficient string assembly

```go
var b strings.Builder            // zero value is ready — no constructor
for _, t := range r.tools {
	fmt.Fprintf(&b, "- %s: %s\n", t.Name(), t.Description())
}
return b.String()
```

- Strings are **immutable**, so `s += x` in a loop reallocates every iteration (quadratic). `Builder` grows one buffer — the idiomatic fix.
- It satisfies `io.Writer`, which is why `fmt.Fprintf(&b, ...)` works (same interface files/sockets use). Pass `&b` because `Write` mutates.

## 5. Running subprocesses — `os/exec` + `context`

```go
ctx, cancel := context.WithTimeout(ctx, r.Timeout)
defer cancel()
c := exec.CommandContext(ctx, fields[0], fields[1:]...)
c.Dir = r.BaseDir
out, err := c.CombinedOutput()
```

- **`context.WithTimeout`** derives a child context that auto-cancels after the deadline; **`defer cancel()`** always releases its resources (cancel is safe to call twice). `CommandContext` kills the process when the context fires.
- **`fields[1:]...`** — the `...` *spreads* a slice into variadic args (Python's `*args`). `exec.Command(name, args...)` takes the program then each arg separately — never a shell string, so there's no shell-injection surface (and no shell features like pipes either).
- **`c.Dir`** pins the working directory — the cwd-jail.
- **`CombinedOutput`** merges stdout+stderr — what you want to feed back to the model as one observation.

### Distinguishing the three failure modes

```go
if ctx.Err() == context.DeadlineExceeded {           // 1) we timed out
	return Result{IsError: true, Output: out + "\n[timed out]", ExitCode: -1}, nil
}
if err != nil {
	if ee, ok := err.(*exec.ExitError); ok {         // 2) ran, exited non-zero
		return Result{Output: out, ExitCode: ee.ExitCode()}, nil
	}
	return Result{IsError: true, Output: err.Error()}, nil // 3) couldn't even start
}
```

- **Type assertion** `err.(*exec.ExitError)` with comma-ok asks "is this error specifically an exit-code error?" A program that compiled and ran but returned non-zero (a failing smoke-test) is **not a tool error** — it's a normal observation with an exit code the model should react to. Only a couldn't-start (bad binary, etc.) is `IsError`.

## 6. Tools return errors *in* the Result, not as `error`

Every tool does `return Result{IsError: true, Output: err.Error()}, nil` — the Go `error` slot stays `nil`. **Expected, recoverable failures** (bad path, command not allowed, non-zero exit) are *data the loop feeds back to the model*, not exceptions that abort the run. The `(T, error)` return is reserved for failures that should actually stop everything. This is a deliberate design line: "the model's mistake" vs "the program is broken."

## 7. Filesystem guardrails — `path/filepath`

```go
func safeJoin(base, rel string) (string, error) {
	if filepath.IsAbs(rel) { return "", fmt.Errorf("absolute paths not allowed: %s", rel) }
	full, _ := filepath.Abs(filepath.Join(base, rel))
	rbase, _ := filepath.Abs(base)
	if full != rbase && !strings.HasPrefix(full, rbase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes project dir: %s", rel)
	}
	return full, nil
}
```

- `filepath.Join` cleans `..` segments; resolving both paths to **absolute** then prefix-checking is what actually blocks `../../etc/passwd`. Reject absolute paths up front.
- `os.PathSeparator` and `filepath.*` keep it cross-platform (don't hand-concat with `/`).
- **Caveat I hit:** `capOutput` slices with `s[:max]` which counts **bytes, not runes** — it can split a UTF-8 char mid-sequence. Fine for v1 capped plain text; would need `utf8`-aware trimming for correctness.

## 8. The agent loop — slices grow, `_` discards

```go
msgs := []llm.Message{ {Role: llm.RoleSystem, ...}, {Role: llm.RoleUser, ...} }
for i := 1; i <= a.MaxIters; i++ {
	raw, err := a.LLM.Chat(ctx, msgs)
	if err != nil { return err }
	msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: raw})

	call, perr := parseToolCall(raw)
	if perr != nil { /* one corrective re-prompt */ continue }
	if call.Tool == "finish" { return nil }
	res, _ := a.Tools.Run(ctx, call.Tool, call.Args)   // _ = we already encode failure in Result
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: observation(res)})
}
```

- **`msgs = append(msgs, x)`** — `append` may reallocate the backing array and returns the (possibly new) slice, so you **must reassign**. The growing `[]Message` *is* the conversation memory.
- The malformed-reply branch does **one re-prompt then `continue`** — it counts against `MaxIters`, so a broken model can't loop forever. Stop conditions: `finish` · max-iters · `Chat` error.
- `res, _ :=` drops the `error` on purpose — tools put recoverable failure in `Result` (§6), so the loop only cares about the `Result`.

## 9. The tool-call protocol & parser — last block wins

```go
var jsonBlock = regexp.MustCompile("(?s)```json\\s*(.*?)```")
m := jsonBlock.FindAllStringSubmatch(raw, -1)
// take m[len(m)-1] — the LAST ```json block
```

- **`(?s)`** makes `.` match newlines (so a multi-line JSON body is captured); **`.*?`** is non-greedy so it stops at the first closing fence, not the last in the whole reply.
- Taking the **last** block is intentional: models often "think out loud" with example blocks first, then emit the real call. Verified live — the 3B model emitted three blocks and the loop correctly acted on the final `finish`.
- The protocol is a **custom text contract**, not provider-native function calling, so it works identically across Ollama/Gemini and keeps `LLMClient` plain text-in/text-out.

---

### Checkpoint 3 — passed ✅
```bash
go build ./... && go run ./cmd/codemypaper run 2401.00001 --ollama-model qwen2.5-coder:3b
```
CLI → `LLMClient` interface → Ollama HTTP → parse → dispatch → `finish`. Machinery proven; intelligence is M3's problem.

### Gotchas worth keeping
- Unused **imports/locals are compile errors** (function params and package-level funcs are exempt).
- **`nil` map write panics**; always initialize before inserting.
- **Map iteration order is randomized** — sort keys for stable output.
- `append` **returns** the slice — reassign it.
- Tools encode failure in `Result.IsError`/`ExitCode`, *not* the `error` return — only "the program is broken" uses `error`.
