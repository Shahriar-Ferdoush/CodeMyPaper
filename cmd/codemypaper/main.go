package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"codemypaper/internal/agent"
	"codemypaper/internal/llm"
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
	var ollamaModel string
	cmd := &cobra.Command{
		Use:   "run <arxiv-url>",
		Short: "Generate an implementation of an arXiv paper",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := llm.NewOllama(ollamaModel)
			fmt.Println("LLM backend:", client.Name())

			// Temporary scratch run to exercise the loop end-to-end (M2).
			// The real paper-driven prompt arrives in M3.
			out := "./out/scratch"
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}

			reg := tools.NewRegistry()
			reg.Register(tools.WriteFile{BaseDir: out})
			reg.Register(tools.ReadFile{BaseDir: out})
			reg.Register(tools.RunCommand{BaseDir: out, Timeout: 120 * time.Second})

			ag := &agent.Agent{LLM: client, Tools: reg, MaxIters: 6, Verbose: true}
			sys := "You can call tools by emitting exactly one ```json block, e.g. " +
				"```json\n{\"tool\":\"write_file\",\"args\":{\"path\":\"hello.py\",\"content\":\"print('hi')\"}}\n```\n" +
				"Available tools:\n" + reg.Descriptions() +
				"When the task is complete, emit ```json\n{\"tool\":\"finish\",\"args\":{\"summary\":\"...\"}}\n```"
			return ag.Run(cmd.Context(), sys,
				"Write hello.py that prints hi, run it with `python3 hello.py`, then finish.")
		},
	}
	cmd.Flags().StringVar(&ollamaModel, "ollama-model", "qwen2.5-coder:3b", "local model id")

	return cmd
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
