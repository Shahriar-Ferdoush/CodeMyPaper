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

// Run drives the act→observe→recover loop: ask the model, parse its tool call,
// run the tool, feed the observation back, repeat until finish or max-iters.
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
		if perr != nil { // malformed → one corrective re-prompt (counts against max-iters)
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
