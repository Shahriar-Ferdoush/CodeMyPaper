package agent

import (
	"context"
	"fmt"

	"codemypaper/internal/llm"
	"codemypaper/internal/log"
	"codemypaper/internal/tools"
)

type Config struct {
	MaxIters int
}

type Outcome struct {
	Success    bool
	Iterations int
	StopReason string
	Summary    string
	Method     string
	Entrypoint string
}

// Agent wires a chat backend to a set of tools and drives the loop.
type Agent struct {
	llm llm.LLMClient
	reg *tools.Registry
	cfg Config
	log *log.Logger
}

func New(client llm.LLMClient, reg *tools.Registry, cfg Config, logger *log.Logger) *Agent {
	return &Agent{llm: client, reg: reg, cfg: cfg, log: logger}
}

// Run drives the act→observe→recover loop: ask the model, parse its tool call,
// run the tool, feed the observation back, repeat until finish or max-iters.
func (a *Agent) Run(ctx context.Context, systemPrompt, task string) (Outcome, error) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: task},
	}

	for iter := 1; iter <= a.cfg.MaxIters; iter++ {
		a.log.Debugf("iteration %d/%d", iter, a.cfg.MaxIters)

		raw, err := a.llm.Chat(ctx, messages)
		if err != nil {
			return Outcome{Iterations: iter, StopReason: "fatal_error"}, fmt.Errorf("chat: %w", err)
		}
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: raw})

		call, perr := parseToolCall(raw)
		if perr != nil {
			a.log.Debugf("malformed turn: %v", perr)
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: protocolReminder(perr)})
			continue
		}

		if call.Name == "finish" {
			a.log.Infof("agent finished after %d iteration(s)", iter)
			return Outcome{
				Success:    true,
				Iterations: iter,
				StopReason: "finished",
				Summary:    argOrEmpty(call.Args, "summary"),
				Method:     argOrEmpty(call.Args, "method"),
				Entrypoint: argOrEmpty(call.Args, "entrypoint"),
			}, nil
		}

		res, _ := a.reg.Run(ctx, call.Name, call.Args) // act
		a.log.Debugf("tool %s -> isError=%v exit=%d", call.Name, res.IsError, res.ExitCode)
		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: observation(call.Name, res)}) // observe
	}

	a.log.Infof("agent stopped: reached max iterations (%d)", a.cfg.MaxIters)
	return Outcome{Iterations: a.cfg.MaxIters, StopReason: "max_iters"}, nil
}

func argOrEmpty(args map[string]any, key string) string {
	if s, ok := args[key].(string); ok {
		return s
	}
	return ""
}

// protocolReminder is the single corrective message sent after a malformed turn.
func protocolReminder(err error) string {
	return fmt.Sprintf("Your previous reply could not be parsed as a tool call (%v). "+
		"Reply with exactly one ```json block containing a \"tool\" field and an \"args\" object, and nothing else after it.", err)
}

// observation renders a tool Result as the user-role message the model reads next.
func observation(toolName string, res tools.Result) string {
	status := "ok"
	if res.IsError {
		status = "error"
	}
	return fmt.Sprintf("[observation of %s | status=%s | exit=%d]\n%s", toolName, status, res.ExitCode, res.Output)
}
