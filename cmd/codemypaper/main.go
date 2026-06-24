package main

import (
	"context"
	"fmt"
	"os"

	"codemypaper/internal/llm"
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

			reply, err := client.Chat(cmd.Context(), []llm.Message{
				{Role: llm.RoleSystem, Content: "You are a helpful assistant that generates runnable code from arXiv papers."},
				{Role: llm.RoleUser, Content: fmt.Sprintf("Please generate a runnable implementation for the paper at %s", args[0])},
			})
			if err != nil {
				return err
			}
			fmt.Println("LLM reply:", reply)
			return nil
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
