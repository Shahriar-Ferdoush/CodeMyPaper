# Code walkthrough: `codemypaper run <arxiv-id>`

The actual call path for one real invocation of the **fixed two-call pipeline** (DESIGN §4/D10):

```
go run ./cmd/codemypaper run 1706.03762 --model gemini
```

`--model gemini` is the default. `--model` is consulted once (`buildClient` constructs the backend); after
that the only backend-dependent call at runtime is `Chat`, invoked at most twice by the pipeline.

The paper's journey through the types:
id string → `*arxiv.Paper` (freshly fetched, or rebuilt offline from a cached source — DESIGN D11) →
prompt text → `[]llm.Message` → raw model reply → `pipeline.Reply{Method, Files}` → files on disk →
`testResult` → `pipeline.Outcome` → exit code.

Control flow lives entirely in Go. The model is a function called at fixed points (generate, then maybe
debug); it never chooses an action, so there is no loop, no tool dispatch, and no turn budget.

````
main()                                                          cmd/codemypaper/main.go:300
 // takes os.Args from the shell → builds the root cobra command; maps the error from
 // ExecuteContext to os.Exit (0 ok · 1 no-green · 2 usage · 3 fatal)
 └─ root.ExecuteContext(ctx)
     // "run 1706.03762 --model gemini" → parses flags, dispatches args=["1706.03762"] to run
     └─ runCmd().RunE                                           cmd/codemypaper/main.go:68
         // args[0]="1706.03762" + parsed flags → returns nil or *exitError{code}; builds the
         // stderr logger and wires everything below
         │
         ├─ buildClient(model, geminiModel, ollamaModel)        cmd/codemypaper/main.go:270
         │   // --model="gemini" + model-id flags → an llm.LLMClient consumed by pipeline.Run;
         │   // a value that isn't gemini/ollama → exit 2
         │   └─ llm.NewGemini(geminiModel)                      internal/llm/gemini.go
         │       // model id "gemini-2.5-flash" → *Gemini holding GEMINI_API_KEY from env;
         │       // no network call yet — key checked at first Chat
         │
         ├─ arxiv.ParseID(args[0])                              internal/arxiv/parse.go:27
         │   // any form (id / abs-URL / pdf-URL / vN suffix) → canonical "1706.03762";
         │   // called directly (not via Fetch) so outDir can be computed before deciding
         │   // whether a fetch is even needed
         │
         ├─ os.MkdirAll(outDir)                                 cmd/codemypaper/main.go:84
         │   // outDir (default ./out/<id> = ./out/1706.03762) → creates the directory
         │   // that becomes the pipeline's OutDir (the path-jail root)
         │
         ├─ logger.AttachFile(outDir/run.log)                   cmd/codemypaper/main.go:90
         │   // tees the stderr logger into out/<id>/run.log — the forensic record; a failure
         │   // here is logged but never fails the run. defer logger.Close().
         │
         ├─ loadOrFetchPaper(ctx, logger, outDir, id, ...)      cmd/codemypaper/main.go:158
         │   // cache-or-fetch (DESIGN D11): tries an offline rebuild first, falls back to a
         │   // real fetch — either path converges on the same *arxiv.Paper for pipeline.Run
         │   │
         │   ├─ [!refetch] loadCachedPaper(logger, outDir, id)  cmd/codemypaper/main.go:187
         │   │   // reads outDir/paper.meta.json + the raw source file it names; any problem
         │   │   // (missing, corrupt, unparsable) is logged and treated as a cache miss, never
         │   │   // a hard error
         │   │   └─ [hit] arxiv.FromCache(id, meta, raw)        internal/arxiv/cache.go:28
         │   │       // dispatches on meta.RawName to the same pure parser Fetch would have used
         │   │       // (htmlToSections / extractEprintTeX+latexSections) — zero network calls
         │   │
         │   └─ [miss/--refetch] arxiv.Fetch(ctx, idOrURL)      internal/arxiv/fetch.go:45
         │       // raw "1706.03762" → *Paper{ID, Title, Abstract, Sections, Source, Raw, RawName};
         │       // ErrSourcesExhausted → exit 3, any other resolve error → exit 2; source ladder
         │       // (D1): arxiv.org/html → ar5iv → e-print tarball, first non-empty wins; title
         │       // and abstract always come from the Atom API as a backstop, then persistPaper
         │       // (main.go:224) removes any stale other-rung file and writes the fresh source +
         │       // paper.meta.json — the sidecar the next rerun's cache hit will read
         │
         └─ pipeline.Run(ctx, client, paper, cfg, logger)       internal/pipeline/pipeline.go:64
             // cfg = Config{OutDir, TestTimeout (--timeout), MaxContextChars}. Returns
             // (Outcome, error); the error is non-nil ONLY for a fatal chat-backend failure.
             // Everything else is encoded in Outcome, and IMP_DETAILS.md is written on every ending.
             │
             ├─ seed messages                                   pipeline.go:65
             │   // []llm.Message{ system(buildSystemPrompt), user(generateTask) }
             │   ├─ buildSystemPrompt(paper, maxContextChars)   internal/pipeline/prompt.go:35
             │   │   // renders prompt.md (//go:embed) with paper.PromptText(maxChars) →
             │   │   // role + success bar + §5 file-block format + hard rules + delimited paper
             │   │   └─ paper.PromptText(maxChars)              internal/arxiv/paper.go:51
             │   │       // char budget → Title + Abstract + MethodRelevant sections, trimmed
             │   └─ generateTask(paper)                         internal/pipeline/prompt.go:52
             │       // "Implement the core method of arXiv:<id> (<title>). Reply with the
             │       //  METHOD line and the required file blocks."
             │
             ├─ os.Remove(outDir/ERROR_LOG.md)                  pipeline.go:73
             │   // a stale error log from a previous run must not survive next to this run's
             │   // IMP_DETAILS.md — a green rerun would otherwise report two contradicting endings
             │
             ├─ [LLM CALL 1/2 — generate]
             │  chatForFiles(&messages, validateGenerate)       pipeline.go:85 → :176
             │   // one logical call: Chat → parseReply → validate, with ≤1 corrective re-prompt.
             │   // Appends every message it sends/receives to *messages so call #2 continues the
             │   // same conversation. Returns (Reply, rawReply, error).
             │   │  loop attempt = 1..2:
             │   ├─ client.Chat(ctx, *messages)                 internal/llm/gemini.go:83
             │   │   // the WHOLE history → raw reply text, appended back as the assistant turn.
             │   │   // The ONLY backend-dependent call. (--model ollama: Ollama.Chat, ollama.go:60,
             │   │   // POSTs the same []Message verbatim — everything else identical.)
             │   │   └─ foldMessages(messages)                  internal/llm/gemini.go:143
             │   │       // Gemini has no system role → system text folds into the first user turn
             │   │   // Chat error → returns fmt.Errorf("chat: %w") → Run maps it to fatal_error
             │   ├─ parseReply(raw)                             internal/pipeline/parse.go:38
             │   │   // pure string→value: strips one outer ``` fence (stripOuterFence, :96),
             │   │   // reads the METHOD: line, then accumulates each "=== FILE: x ===" …
             │   │   // "=== END FILE ===" block into Reply.Files. Markers match whole-line-exact
             │   │   // (isFileMarker, :83) so file content can't truncate a block.
             │   ├─ validateGenerate(reply, outDir, reserved)   pipeline.go:214
             │   │   // model.py + smoke_test.py + main.py all present, then validateNames:
             │   │   └─ validateNames(files, outDir, reserved)  pipeline.go:255
             │   │       // every name through safeJoin (jail.go — no ../absolute) + the
             │   │       // case-insensitive reserved-name check (run.log/IMP_DETAILS.md/ERROR_LOG.md/
             │   │       // paper source can't be clobbered)
             │   └─ [invalid] correctiveTask(err) appended, retry once   internal/pipeline/prompt.go:82
             │       // the re-prompt text IS the validation error. Still bad after attempt 2 →
             │       // returns errMalformed + the raw reply.
             │      // errMalformed → writeErrorLog(StopMalformed, raw) → finish → exit 1  pipeline.go:91
             │      // other Chat error   → writeImpDetails + return err → classifyRunError (exit 2/3)
             │
             ├─ writeFiles(outDir, reply.Files, &rep)           pipeline.go:99 → :298
             │   // each file re-jailed via safeJoin (belt-and-braces), MkdirAll for nested paths,
             │   // os.WriteFile; names recorded on the run report
             │
             ├─ first := runSmokeTest(ctx, outDir, timeout)     pipeline.go:106 → run.go:44
             │   // exec.CommandContext("python3", "smoke_test.py") with cwd=outDir, the --timeout
             │   // deadline, and combined stdout+stderr. ALL failure modes fold into one
             │   // testResult{Output, ExitCode, TimedOut, Duration}: timeout → ExitCode -1,
             │   // exec.ExitError → its code, python3 missing → captured. Output capped at 8 KiB
             │   // (capOutput, run.go:86). Verdict is a single first.passed() (run.go:31).
             │   └─ [first.passed()] → finish(Outcome{Success, StopPassed})  pipeline.go:110
             │       // IMP_DETAILS.md written, return. ONE LLM call total, exit 0. ← the green path
             │
             ├─ [first failed] append debugTask(first)          pipeline.go:120 → prompt.go:69
             │   // a user message carrying the failing output + exit code, and "rewrite only the
             │   // file blocks you are changing; do not introduce new files"
             │
             ├─ [LLM CALL 2/2 — debug]
             │  chatForFiles(&messages, validateDebug)          pipeline.go:122
             │   // same chat/parse/validate/≤1-re-prompt machinery, but validateDebug (pipeline.go:232)
             │   // rejects any file that wasn't part of the generate output — a repair may only
             │   // rewrite existing files, never invent new ones.
             │   // errMalformed here → writeErrorLog(StopMalformed, raw, FirstFail=first.Output)
             │   //   so the log keeps the failure that triggered the repair → finish → exit 1
             │
             ├─ writeFiles(outDir, fixed.Files, nil)            pipeline.go:137
             │   // overwrites the rewritten subset
             │
             └─ second := runSmokeTest(ctx, outDir, timeout)    pipeline.go:144
                 ├─ [second.passed()] → finish(Outcome{Success, StopPassed})  pipeline.go:148
                 │   // exit 0 after the single repair ← the red→green path
                 └─ [second failed] → StopRepairExhausted                     pipeline.go:151
                     ├─ writeErrorLog(outDir, o, rewritten)     output_imp_details.go:107
                     │   // both failing outputs + which files the repair rewrote
                     └─ finish(o)  → IMP_DETAILS.md → exit 1                       pipeline.go:159

 // finish closure (pipeline.go:77): on EVERY ending, sets rep.Outcome and calls
 //   writeImpDetails(outDir, rep) (output_imp_details.go:39) → IMP_DETAILS.md (paper, backend, method, entrypoint
 //   main.py, files, per-run verdicts, stop reason + mapped exit code, verbatim success bar).
 //   Best-effort — a write failure is logged, never fatal: the file can’t break the run it describes.

 back in RunE:                                                  cmd/codemypaper/main.go:116
 // prints "outcome: <StopReason> (success=<bool>)" + method; !Success → exit 1;
 // a non-nil error → classifyRunError (ErrNoAPIKey → 2, backend/other → 3)  main.go:287
 back in main():                                                cmd/codemypaper/main.go:300
 // unwraps *exitError → os.Exit(code): 0 green · 1 no-green · 2 usage · 3 fatal
````

**Why this shape (DESIGN D10):** the task graph is static — generate → test → one repair → test → exit — so
the standard workflow-vs-agent rule puts control flow in Go, not in the model. A run is exactly 1–2 logical
LLM calls (plus at most one corrective re-prompt each), every failure is attributable to a stage, and there is
no turn budget for a model to wander through. The debug call continues the generate conversation, so the model
still has the paper and its own files in context; only the failing output is new. D9's single-repair policy
(`ERROR_LOG.md`, exit 1) is preserved as the pipeline's shape rather than a counter.
