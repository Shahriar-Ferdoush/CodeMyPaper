package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"codemypaper/internal/agent"
	"codemypaper/internal/arxiv"
	"codemypaper/internal/llm"
	"codemypaper/internal/log"
	"codemypaper/internal/tools"
	"github.com/spf13/cobra"
)

var version = "dev"

// exitError carries a process exit code up to main alongside the underlying error.
type exitError struct {
	code int
	err  error
}

// Error returns the underlying error's message, or "" if err is nil.
func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

// exitErr wraps err with the process exit code it should map to.
func exitErr(code int, err error) error { return &exitError{code: code, err: err} }

// versionCmd builds the `version` subcommand.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of CodeMyPaper",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Println(version)
		},
	}
}

// runCmd builds the `run` subcommand: fetch the arXiv paper, wire the tool registry
// and agent, and drive the loop to a green smoke-test or max-iters.
func runCmd() *cobra.Command {
	var (
		model           string
		geminiModel     string
		ollamaModel     string
		outDir          string
		maxIters        int
		cmdTimeout      time.Duration
		maxContextChars int
		verbose         bool
	)
	cmd := &cobra.Command{
		Use:   "run <arxiv-id-or-url>",
		Short: "Generate an implementation of an arXiv paper",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if maxIters < 1 {
				return exitErr(2, fmt.Errorf("--max-iters must be at least 1, got %d", maxIters))
			}
			logger := log.New(os.Stderr, verbose)
			ctx := cmd.Context()

			client, err := buildClient(model, geminiModel, ollamaModel)
			if err != nil {
				return err
			}

			logger.Infof("fetching arXiv:%s ...", args[0])
			paper, err := arxiv.Fetch(ctx, args[0])
			if err != nil {
				if errors.Is(err, arxiv.ErrSourcesExhausted) {
					return exitErr(3, fmt.Errorf("fetch paper: %w", err))
				}
				return exitErr(2, fmt.Errorf("resolve paper: %w", err))
			}
			logger.Infof("fetched %q via %s (%d sections)",
				paper.Title, paper.Source, len(paper.Sections))

			if outDir == "" {
				outDir = filepath.Join("out", paper.ID)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return exitErr(3, fmt.Errorf("create out dir: %w", err))
			}

			reg := tools.NewRegistry()
			reg.Register(tools.NewWriteFile(outDir, logger))
			reg.Register(tools.NewReadFile(outDir, logger))
			reg.Register(tools.NewRunCommand(outDir, cmdTimeout, logger))

			logger.Infof("backend=%s out=%s max-iters=%d", client.Name(), outDir, maxIters)

			a := agent.New(client, reg, agent.Config{MaxIters: maxIters}, logger)
			outcome, err := a.Run(ctx, agent.BuildSystemPrompt(reg, paper, maxContextChars), agent.FirstUserMessage(paper))
			if err != nil {
				return classifyRunError(err)
			}

			fmt.Printf("\noutcome: %s (success=%v, iterations=%d)\n",
				outcome.StopReason, outcome.Success, outcome.Iterations)
			if outcome.Summary != "" {
				fmt.Println("summary:", outcome.Summary)
			}
			if !outcome.Success {
				return exitErr(1, nil)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&model, "model", "gemini", "backend: gemini or ollama")
	cmd.Flags().StringVar(&geminiModel, "gemini-model", "gemini-2.5-flash", "hosted model id (gemini backend)")
	cmd.Flags().StringVar(&ollamaModel, "ollama-model", "qwen2.5-coder:3b", "local model id (ollama backend)")
	cmd.Flags().StringVar(&outDir, "out", "", "output directory; defaults to ./out/<arxiv-id>")
	cmd.Flags().IntVar(&maxIters, "max-iters", 6, "max agent iterations")
	cmd.Flags().DurationVar(&cmdTimeout, "timeout", 120*time.Second, "per-command timeout")
	cmd.Flags().IntVar(&maxContextChars, "max-context-chars", 60000, "paper context budget in characters")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream the agent loop to stderr")

	return cmd
}

// buildClient selects the LLMClient backend from --model.
// Input:
//   - model: "gemini" or "ollama"
//   - geminiModel, ollamaModel: the hosted/local model id for the selected backend
//
// Output:
//   - llm.LLMClient: the constructed backend
//   - error: exitErr(2, ...) if model is neither "gemini" nor "ollama"
func buildClient(model, geminiModel, ollamaModel string) (llm.LLMClient, error) {
	switch model {
	case "gemini":
		return llm.NewGemini(geminiModel), nil
	case "ollama":
		return llm.NewOllama(ollamaModel), nil
	default:
		return nil, exitErr(2, fmt.Errorf("--model must be \"gemini\" or \"ollama\", got %q", model))
	}
}

// classifyRunError maps an Agent.Run error to a process exit code.
// Input:
//   - err: the error returned by Agent.Run
//
// Output:
//   - error: an *exitError with code 2 for a missing API key, 3 for any other backend/loop failure
func classifyRunError(err error) error {
	switch {
	case errors.Is(err, llm.ErrNoAPIKey):
		return exitErr(2, err)
	case errors.Is(err, llm.ErrBackendUnreachable), errors.Is(err, llm.ErrRateLimited):
		return exitErr(3, err)
	default:
		return exitErr(3, err)
	}
}

// main runs the CLI and maps the returned error to a process exit code (0 ok · 1 no
// green smoke-test · 2 usage/config · 3 fatal).
func main() {
	root := &cobra.Command{
		Use:           "codemypaper",
		Short:         "Turn an arXiv paper into a runnable reference implementation",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(versionCmd(), runCmd())

	if err := root.ExecuteContext(context.Background()); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.err != nil {
				fmt.Fprintln(os.Stderr, "error:", ee.err)
			}
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
}
