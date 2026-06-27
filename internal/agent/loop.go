package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"codemypaper/internal/llm"
	"codemypaper/internal/tools"
)

type Agent struct {
	LLM      llm.LLMClient
	Tools    *tools.Registry
	MaxIters int
	Verbose  bool

	// Transcript, when non-nil, receives one JSON line per message
	// (transcript.jsonl, §7). No secrets flow through messages.
	Transcript io.Writer
}

// StopReason names why the loop stopped. It is part of the run contract: main
// maps it to an exit code and RUN_SUMMARY records it verbatim.
type StopReason string

const (
	StopFinished   StopReason = "finished"    // model called finish
	StopMaxIters   StopReason = "max_iters"   // iteration budget exhausted
	StopWallBudget StopReason = "wall_budget" // wall-clock budget exhausted
	StopFatal      StopReason = "fatal"       // unrecoverable error (paired with a non-nil error)
)

// Outcome is the structured result of a run. It carries everything the exit-code
// mapping (PROJECT_SPECIFICATION §4) and RUN_SUMMARY.md (§7) need.
type Outcome struct {
	Reason     StopReason
	Iterations int
	Summary    string // from finish args
	Method     string // from finish args
	Entrypoint string // from finish args
	Green      bool   // the last smoke-test (python/pytest) command exited 0
}

// Succeeded is the contract-level success test: the model finished AND a
// smoke-test was actually observed green. A finish on a red/absent test is NOT
// success — exit code 1, not 0.
func (o Outcome) Succeeded() bool { return o.Reason == StopFinished && o.Green }

// isTestCommand reports whether a run_command invocation is running the
// smoke-test (vs. pip/ls/cat). Its exit status is what defines "green".
func isTestCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "python", "python3", "pytest":
		return true
	default:
		return false
	}
}

// Run drives the act→observe→recover loop: ask the model, parse its tool call,
// run the tool, feed the observation back, repeat until finish, max-iters, or
// the context (wall budget) is cancelled.
//
// "Green" is tracked independently of the model's own finish claim: it is the
// exit status of the most recent python/pytest run_command. finish is only a
// success if a green test was actually observed.
func (a *Agent) Run(ctx context.Context, systemPrompt, task string) (Outcome, error) {
	var msgs []llm.Message
	// add appends a message and mirrors it to the transcript (when enabled), so
	// every turn is logged from a single place.
	add := func(m llm.Message) {
		msgs = append(msgs, m)
		a.record(m)
	}
	add(llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	add(llm.Message{Role: llm.RoleUser, Content: task})

	var ranTest, lastTestGreen bool
	green := func() bool { return ranTest && lastTestGreen }

	for i := 1; i <= a.MaxIters; i++ {
		if ctx.Err() != nil {
			fmt.Println("\n⏹ stopped: wall budget exhausted")
			return Outcome{Reason: StopWallBudget, Iterations: i - 1, Green: green()}, nil
		}

		raw, err := a.LLM.Chat(ctx, msgs)
		if err != nil {
			// A cancelled context (wall budget) surfaces here as a transport
			// error; report it as budget exhaustion, not a fatal backend error.
			if ctx.Err() != nil {
				fmt.Println("\n⏹ stopped: wall budget exhausted")
				return Outcome{Reason: StopWallBudget, Iterations: i - 1, Green: green()}, nil
			}
			return Outcome{Reason: StopFatal, Iterations: i - 1, Green: green()}, err
		}
		add(llm.Message{Role: llm.RoleAssistant, Content: raw})
		if a.Verbose {
			fmt.Printf("\n--- iter %d ---\n%s\n", i, raw)
		}

		call, perr := parseToolCall(raw)
		if perr != nil { // malformed → one corrective re-prompt (counts against max-iters)
			add(llm.Message{Role: llm.RoleUser,
				Content: "Your reply had no valid tool call. Reply with exactly one ```json {\"tool\":...,\"args\":...}``` block."})
			continue
		}

		if call.Tool == "finish" {
			summary, _ := call.Args["summary"].(string)
			method, _ := call.Args["method"].(string)
			entrypoint, _ := call.Args["entrypoint"].(string)
			if green() {
				fmt.Println("\n✅ finished:", summary)
			} else {
				fmt.Println("\n⚠ model called finish, but no green smoke-test was observed")
			}
			return Outcome{
				Reason:     StopFinished,
				Iterations: i,
				Summary:    summary,
				Method:     method,
				Entrypoint: entrypoint,
				Green:      green(),
			}, nil
		}

		res, err := a.Tools.Run(ctx, call.Tool, call.Args)
		if err != nil {
			// A tool returning a hard error is non-fatal: surface it as an
			// observation so the model can recover, rather than dropping it.
			res = tools.Result{IsError: true, Output: err.Error()}
		}
		if call.Tool == "run_command" {
			if cmd, ok := call.Args["cmd"].(string); ok && isTestCommand(cmd) {
				ranTest = true
				lastTestGreen = !res.IsError && res.ExitCode == 0
			}
		}

		obs := fmt.Sprintf("Observation from %s (exit=%d, error=%v):\n%s", call.Tool, res.ExitCode, res.IsError, res.Output)
		add(llm.Message{Role: llm.RoleUser, Content: obs})
	}

	fmt.Println("\n⏹ stopped: max iterations reached")
	return Outcome{Reason: StopMaxIters, Iterations: a.MaxIters, Green: green()}, nil
}

// record writes one JSON line per message to the transcript when enabled.
// Transcript failures are non-fatal: a logging hiccup must not abort a run.
func (a *Agent) record(m llm.Message) {
	if a.Transcript == nil {
		return
	}
	line, err := json.Marshal(struct {
		Role    llm.Role `json:"role"`
		Content string   `json:"content"`
	}{m.Role, m.Content})
	if err != nil {
		return
	}
	fmt.Fprintln(a.Transcript, string(line))
}
