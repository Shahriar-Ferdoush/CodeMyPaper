# Code My Paper — Project Brief

> **Purpose of this document:** This is a high-level project brief, not a full spec. It captures *what* we are
> building, *why* we chose it, and a *broad outline* to follow. The detailed requirements, formal specification,
> and architectural decisions will be hammered out in a later pass (likely with Claude on Mac). Be liberal —
> treat everything here as a starting direction, not a contract.

---

## 1. One-line description

A **Go terminal agent** that takes an arXiv paper ID, reads the paper's source, and generates a **runnable
reference implementation of the paper's core method**, self-correcting by running a toy smoke-test until the
code executes.

---

## 2. Why this project (the goals it serves)

This project was chosen deliberately to serve three goals at once:

1. **Learn Go along the way** — through the features that actually make Go worth learning (interfaces,
   goroutines/concurrency, `net/http`, `os/exec`, `context`), not toy exercises. The work also overlaps the
   Go I already need for my day job (zCompose).
2. **CV impact** — adds an identity my CV currently lacks: **backend / systems engineering + agent
   orchestration**, on top of my existing ML/LLM research and Python strengths.
3. **Show a new side that's still uniquely mine** — I'm a researcher who reproduces papers (model merging,
   GPT-2 from scratch, fine-tuning). An agent that turns *papers into code* sits exactly where my research
   identity meets new agent-infra skills. It's not a generic coding-agent clone.

### Background reasoning (how we landed here)

- My CV is heavy on ML/LLM research and Python application glue, but light on **systems/backend**,
  **concurrency**, and the **infrastructure layer of AI**.
- Go is the *industry-standard* language for the orchestration/infra layer of AI tooling — the popular
  open-source terminal coding agents (e.g. OpenCode, Crush) are written in Go, largely thanks to the Charm
  TUI ecosystem. So building a coding agent in Go is the on-trend, correct choice — not forcing Go where
  Python belongs.
- Key split to remember: **Python wins for agent *logic*/prototyping; Go wins for agent *infrastructure*/
  orchestration.** This project lets me credibly claim both halves.

---

## 3. Goals & non-goals (scope guardrails)

These guardrails are what keep the project finishable in ~3–4 days and honest on the CV/README.

**In scope (✅)**
- Implement the *core method / algorithm* of a paper (a loss function, a layer/attention variant, an
  optimizer, a model-merging rule, etc.).
- Produce code that **runs on toy input without crashing**.
- A full **act → observe → recover** agent loop (generate code → run smoke-test → read error → fix → repeat).
- Provider-agnostic model backend (swap local vs hosted with a flag).

**Out of scope (❌)**
- Reproducing full training pipelines, datasets, or matching the paper's reported numbers.
- PDF math/figure extraction (we use arXiv source instead — see decisions below).
- Fancy TUI in v1 (core CLI engine first; TUI is a possible later layer).

**Success bar (state this plainly in the README):**
"Generated code runs on toy input and implements the named method" — **not** "reproduces paper results."

**Best-fit inputs:** ML papers with a self-contained algorithm — my home turf, so I can judge correctness.

---

## 4. Decisions already made

| Decision | Choice | Reason |
|---|---|---|
| Language | **Go** | Learning goal + correct tool for agent orchestration/infra |
| Paper input (v1) | **arXiv ID/URL → fetch source (LaTeX/HTML)** | Avoids the PDF-parsing rabbit hole; clean text in, fast feasibility |
| Model — plumbing/dev | **Local ~3B via Ollama** | Free, offline, fast iteration; intelligence doesn't matter while wiring the loop |
| Model — real runs | **Gemini free tier (default)**, pluggable to Groq/Deepseek/Claude | Generous free quota + large context for long papers; keep it swappable |
| v1 polish | **Core CLI engine first** | Get the agent loop working end-to-end before any TUI |

**Cost target:** ~$0. Build/debug on local Ollama (free); use free hosted tiers for real implementation runs.

---

## 5. High-level functional requirements (liberal / to be refined)

1. **Input:** Accept an arXiv ID or URL (later: paste-text and local-PDF as alternates).
2. **Paper ingestion:** Fetch and extract clean source text of the paper's method section(s).
3. **Method extraction:** Identify the core algorithm/method/equations to implement.
4. **Code generation:** Produce one or more runnable code files (default target language: Python/PyTorch,
   since that's where ML reference implementations live and where I can verify correctness).
5. **Tool use:** The agent can `write_file` and `run_command` (run a toy smoke-test).
6. **Self-correction:** On smoke-test failure, feed the error back and retry up to N times.
7. **Model selection:** `--model` flag chooses backend (ollama / gemini / …).
8. **Output:** A small generated project folder + a short summary of what was implemented and any caveats.

---

## 6. Architecture sketch (to be detailed later)

```
codemypaper/
├─ cmd/codemypaper/main.go      # CLI entry (Cobra) — flags: --arxiv, --model, --out
├─ internal/
│  ├─ agent/loop.go             # THE agent loop (act → observe → recover)
│  ├─ agent/prompt.go           # system prompt + tool schemas
│  ├─ llm/client.go             # LLMClient interface  ← the key abstraction
│  ├─ llm/ollama.go             # local 3B (free plumbing dev)
│  ├─ llm/gemini.go             # real runs (free tier)
│  ├─ tools/tools.go            # Tool interface + registry
│  ├─ tools/writefile.go        # write generated code
│  ├─ tools/runcmd.go           # run smoke-test, capture stdout/stderr
│  └─ arxiv/fetch.go            # arXiv ID → clean source text
└─ README.md                    # with the honest scope + a demo recording
```

The core engine, conceptually (~40 lines of Go):

```
for {
    resp := llm.Chat(messages, tools)        // ask the model
    if resp.IsToolCall {
        result := registry.Run(resp.Tool)    // write file / run smoke-test
        messages.append(toolResult(result))  // observe
        continue                             // recover & retry
    }
    break                                    // model says "done"
}
```

Two abstractions carry the whole design:
- **`LLMClient` interface** → one interface, many backends (Ollama, Gemini, …). Selecting a backend is a flag.
- **`Tool` interface + registry** → the agent's hands (write file, run command), easy to extend.

---

## 7. Learning outline (Go concepts mapped to milestones)

This doubles as the build order. Days 1–2 run entirely on free local Ollama; only Day 3+ touches a hosted tier.

| Day | Build | Go concept learned | Agent concept learned |
|----|-------|--------------------|------------------------|
| **1** | CLI skeleton (Cobra) + `LLMClient` interface + Ollama impl; one chat round-trip | structs, **interfaces**, packages, `net/http`, JSON | LLM as a swappable backend |
| **2** | `Tool` interface + `write_file`/`run_command` + the agent loop (driven by dumb local 3B) | **interfaces + registry map**, `os/exec`, error handling | tool-calling, act→observe loop |
| **3** | arXiv fetch (ID → source text) + real system prompt; swap to Gemini for a real run | `context`, HTTP clients, string handling | grounding the agent in a document |
| **4** | Smoke-test self-correction loop + polish (README, scope, demo recording) | loops, `select`/timeouts, cleanup | **self-correction** (the impressive part) |

Beyond Go syntax, the project builds comfort with the broader toolkit: **agent loops, LLM tool-calling,
provider abstraction, running/observing subprocesses, and packaging a CLI** — i.e. getting comfortable with
"tools, agents, and skills" generally.

---

## 8. Open questions for the detailed spec pass

- Exact arXiv source-fetch strategy (ar5iv HTML vs e-print LaTeX tar vs abstract+sections only).
- How to chunk/trim long papers to fit context windows (esp. for smaller hosted models).
- Tool-calling format: native function-calling API vs a simple JSON/text protocol parsed by us
  (the latter is more portable across Ollama/Gemini/etc.).
- Smoke-test generation: who writes the toy test — the agent, or a fixed harness?
- Retry/stop conditions and guardrails for `run_command` (timeouts, sandboxing, allowed commands).
- Output target language(s) — default Python/PyTorch; keep extensible?
- Eval: how do we (lightly) judge "did it implement the method," beyond "it runs"?

---

## 9. CV framing (target wording)

> **Code My Paper** (Go, LLM agents) — A terminal agent that converts arXiv ML papers into runnable
> reference implementations of their core method. Built a provider-agnostic agent loop with tool-calling,
> command execution, and self-correction; pluggable across local (Ollama) and hosted (Gemini) models.

This single line surfaces: **Go / systems**, **agent orchestration**, **LLM tool-use**, and leans on existing
research credibility — covering the exact gaps identified in §2.
