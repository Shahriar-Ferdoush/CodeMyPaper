package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"codemypaper/internal/agent"
	"codemypaper/internal/llm"
	"codemypaper/internal/log"
	"codemypaper/internal/tools"
	"github.com/spf13/cobra"
)

var version = "dev"

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of CodeMyPaper",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Println(version)
		},
	}
}

func runCmd() *cobra.Command {
	var (
		ollamaModel string
		outDir      string
		maxIters    int
		cmdTimeout  time.Duration
		verbose     bool
	)
	cmd := &cobra.Command{
		Use:   "run <arxiv-url>",
		Short: "Generate an implementation of an arXiv paper",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if maxIters < 1 {
				return fmt.Errorf("--max-iters must be at least 1, got %d", maxIters)
			}
			logger := log.New(os.Stderr, verbose)

			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("create out dir: %w", err)
			}

			reg := tools.NewRegistry()
			reg.Register(tools.NewWriteFile(outDir, logger))
			reg.Register(tools.NewReadFile(outDir, logger))
			reg.Register(tools.NewRunCommand(outDir, cmdTimeout, logger))

			client := llm.NewOllama(ollamaModel)
			logger.Infof("backend=%s out=%s max-iters=%d", client.Name(), outDir, maxIters)

			task := fmt.Sprintf("(arXiv ref: %s) Write hello.py that prints a greeting, "+
				"run it with run_command, confirm the output, then finish.", args[0])

			a := agent.New(client, reg, agent.Config{MaxIters: maxIters}, logger)
			outcome, err := a.Run(cmd.Context(), buildSystemPrompt(reg), task)
			if err != nil {
				return err
			}

			fmt.Printf("\noutcome: %s (success=%v, iterations=%d)\n",
				outcome.StopReason, outcome.Success, outcome.Iterations)
			if outcome.Summary != "" {
				fmt.Println("summary:", outcome.Summary)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&ollamaModel, "ollama-model", "qwen2.5-coder:3b", "local model id")
	cmd.Flags().StringVar(&outDir, "out", "./out", "output directory (the cwd-jail base)")
	cmd.Flags().IntVar(&maxIters, "max-iters", 6, "max agent iterations")
	cmd.Flags().DurationVar(&cmdTimeout, "timeout", 120*time.Second, "per-command timeout")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream the agent loop to stderr")

	return cmd
}

func buildSystemPrompt(reg *tools.Registry) string {
	var b strings.Builder
	b.WriteString("You are a coding agent. Act by emitting exactly one ```json block ")
	b.WriteString("with a \"tool\" field and an \"args\" object. Available tools:\n")
	for _, t := range reg.Tools() {
		fmt.Fprintf(&b, "- %s\n", t.Description())
	}
	b.WriteString("- finish: end the task. args: {\"summary\": string, \"method\": string, \"entrypoint\": string}\n")
	b.WriteString("\nExample:\n```json\n{\"tool\": \"write_file\", \"args\": {\"path\": \"hello.py\", \"content\": \"print('hi')\"}}\n```\n")
	return b.String()
}

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
