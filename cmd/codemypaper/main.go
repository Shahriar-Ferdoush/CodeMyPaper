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
	"codemypaper/internal/report"
	"codemypaper/internal/tools"
	"github.com/spf13/cobra"
)

var version = "dev"

// errFetch tags an arXiv fetch failure so main can map it to exit code 3.
var errFetch = errors.New("fetch failed")

// errUsage tags a usage/config error so main can map it to exit code 2.
var errUsage = errors.New("usage error")

// errNotGreen tags a completed-but-not-green run so main can map it to exit
// code 1 (budget exhausted / not green) — distinct from a fatal error.
var errNotGreen = errors.New("not green")

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
		model             string
		geminiModel       string
		ollamaModel       string
		visionMode        string
		geminiVisionModel string
		groqVisionModel   string
		maxFigures        int
		lang              string
		out               string
		maxIters          int
		timeout           time.Duration
		wallBudget        time.Duration
		maxContextChars   int
		verbose           bool
	)

	cmd := &cobra.Command{
		Use:   "run <arxiv-id-or-url>",
		Short: "Generate an implementation of an arXiv paper",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Resolve the chat backend. The Gemini key is checked here, before
			// any network call, so a keyless gemini run exits 2 immediately.
			var client llm.LLMClient
			switch model {
			case "gemini":
				if os.Getenv("GEMINI_API_KEY") == "" {
					return fmt.Errorf("set it, or use --model ollama for a local backend: %w", llm.ErrNoAPIKey)
				}
				client = llm.NewGemini(geminiModel)
			case "ollama":
				client = llm.NewOllama(ollamaModel)
			default:
				return fmt.Errorf("%w: unknown --model %q (expected \"gemini\" or \"ollama\")", errUsage, model)
			}
			fmt.Println("LLM backend:", client.Name())

			// 1b. Validate the remaining config knobs up front (exit 2 on bad input).
			if lang != "python" {
				return fmt.Errorf("%w: --lang %q unsupported (v1: python only)", errUsage, lang)
			}
			visionBackend := resolveVision(visionMode, model)
			if visionBackend == "" {
				return fmt.Errorf("%w: unknown --vision %q (expected auto|gemini|groq|none)", errUsage, visionMode)
			}
			// NOTE: the vision pre-pass itself is not wired yet; visionBackend is the
			// resolved choice recorded in RUN_SUMMARY. The --gemini-vision-model,
			// --groq-vision-model, and --max-figures knobs feed that pass once built.

			// 1a. Bound the whole run (fetch + loop) by the wall budget. 0 means
			// unlimited, in which case the per-call client timeouts still apply.
			ctx := cmd.Context()
			if wallBudget > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, wallBudget)
				defer cancel()
			}

			// 2. Fetch and trim the paper. Tag failures so they map to exit 3.
			paper, err := arxiv.Fetch(ctx, args[0], maxContextChars)
			if err != nil {
				return fmt.Errorf("%w: %v", errFetch, err)
			}

			// 3. Resolve and create the output directory.
			if out == "" {
				out = filepath.Join("./out", paper.ID)
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}

			// 4. Build the tool registry (the per-command timeout is the run_command
			// exec timeout, not an HTTP timeout).
			reg := tools.NewRegistry()
			reg.Register(tools.WriteFile{BaseDir: out})
			reg.Register(tools.ReadFile{BaseDir: out})
			reg.Register(tools.RunCommand{BaseDir: out, Timeout: timeout})

			// 5. Build the paper-driven prompt + task.
			sys, truncated := agent.BuildSystemPrompt(reg, paper)
			task := agent.BuildTask(paper)

			// 6. Run the loop. With --verbose, mirror every turn to transcript.jsonl.
			ag := &agent.Agent{LLM: client, Tools: reg, MaxIters: maxIters, Verbose: verbose}
			if verbose {
				tf, ferr := os.Create(filepath.Join(out, "transcript.jsonl"))
				if ferr != nil {
					return ferr
				}
				defer tf.Close()
				ag.Transcript = tf
			}
			outcome, runErr := ag.Run(ctx, sys, task)

			// Decide the run result: a fatal Go error, else a not-green stop is a
			// failure (exit 1) even with no Go error — see exitCode/§4.
			var retErr error
			switch {
			case runErr != nil:
				retErr = runErr
			case !outcome.Succeeded():
				retErr = fmt.Errorf("%w: stopped (%s) without a green smoke-test", errNotGreen, outcome.Reason)
			}

			// 7. RUN_SUMMARY.md is ALWAYS written (§7), success or failure.
			writeSummary(out, paper, client.Name(), visionBackend, outcome, truncated, exitCode(retErr))
			return retErr
		},
	}

	f := cmd.Flags()
	f.StringVar(&model, "model", "gemini", "chat backend: gemini | ollama")
	f.StringVar(&geminiModel, "gemini-model", "gemini-2.5-flash", "hosted chat model id")
	f.StringVar(&ollamaModel, "ollama-model", "qwen2.5-coder:3b", "local model id")
	f.StringVar(&visionMode, "vision", "auto", "vision backend: auto | gemini | groq | none")
	f.StringVar(&geminiVisionModel, "gemini-vision-model", "gemini-2.5-flash", "vision model id (gemini)")
	f.StringVar(&groqVisionModel, "groq-vision-model", "llama-3.2-11b-vision", "vision model id (groq)")
	f.IntVar(&maxFigures, "max-figures", 4, "max figures sent to the vision backend")
	f.StringVar(&lang, "lang", "python", "target language (v1: python only)")
	f.StringVar(&out, "out", "", "output directory (default ./out/<arxiv-id>)")
	f.IntVar(&maxIters, "max-iters", 6, "max loop iterations")
	f.DurationVar(&timeout, "timeout", 120*time.Second, "per-command timeout")
	f.DurationVar(&wallBudget, "wall-budget", 10*time.Minute, "total run budget (0 = unlimited)")
	f.IntVar(&maxContextChars, "max-context-chars", 60000, "paper-text budget")
	f.BoolVar(&verbose, "verbose", false, "stream the loop")

	return cmd
}

// resolveVision applies the §4 "--vision auto" rule and validates the mode.
// It returns the chosen backend label ("gemini"|"groq"|"none"), or "" for an
// unknown mode. auto → gemini if chat is gemini with a key; else groq if a Groq
// key is present; else none.
func resolveVision(mode, chatModel string) string {
	switch mode {
	case "none", "gemini", "groq":
		return mode
	case "auto":
		if chatModel == "gemini" && os.Getenv("GEMINI_API_KEY") != "" {
			return "gemini"
		}
		if os.Getenv("GROQ_API_KEY") != "" {
			return "groq"
		}
		return "none"
	default:
		return ""
	}
}

// writeSummary assembles and writes RUN_SUMMARY.md. A summary-write failure is
// reported to stderr but never changes the run's exit code.
func writeSummary(out string, paper *arxiv.Paper, chat, vision string, o agent.Outcome, truncated bool, code int) {
	var caveats []string
	if truncated {
		caveats = append(caveats, "Paper text was truncated to fit the context budget.")
	}
	switch o.Reason {
	case agent.StopMaxIters:
		caveats = append(caveats, "Stopped at the iteration limit; smoke-test not green.")
	case agent.StopWallBudget:
		caveats = append(caveats, "Stopped at the wall-clock budget; smoke-test not green.")
	case agent.StopFatal:
		caveats = append(caveats, "Stopped on a fatal error.")
	case agent.StopFinished:
		if !o.Green {
			caveats = append(caveats, "Model called finish without an observed green smoke-test.")
		}
	}
	err := report.WriteSummary(filepath.Join(out, "RUN_SUMMARY.md"), report.Summary{
		ID:               paper.ID,
		Title:            paper.Title,
		ChatBackend:      chat,
		VisionBackend:    vision,
		Method:           o.Method,
		Entrypoint:       o.Entrypoint,
		Iterations:       o.Iterations,
		ExitCode:         code,
		StopReason:       string(o.Reason),
		FiguresDescribed: len(paper.FigureNotes),
		Truncated:        truncated,
		Caveats:          caveats,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not write RUN_SUMMARY.md:", err)
	}
}

// exitCode maps an error to a PROJECT_SPECIFICATION §4 exit code.
//
//	nil                                    -> 0  (smoke-test green)
//	errors.Is(err, errNotGreen)            -> 1  (budget exhausted, not green)
//	errors.Is(err, ErrNoAPIKey | errUsage) -> 2  (usage/config error)
//	errors.Is(err, errFetch | unreachable) -> 3  (fatal: fetch failed / backend unreachable)
//	otherwise                              -> 1
func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, llm.ErrNoAPIKey), errors.Is(err, errUsage):
		return 2
	case errors.Is(err, errFetch), errors.Is(err, llm.ErrBackendUnreachable):
		return 3
	default:
		return 1
	}
}

func main() {
	root := &cobra.Command{
		Use:   "codemypaper",
		Short: "Turn an arXiv paper into a runnable reference implementation",
	}
	// We print errors ourselves below (single path); silence Cobra's own
	// error/usage dump so runtime failures aren't reported twice.
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.AddCommand(versionCmd(), runCmd())
	if err := root.ExecuteContext(context.Background()); err != nil {
		// Errors here never contain secrets: the API key lives only in the
		// Gemini request URL and is never wrapped into a returned error.
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCode(err))
	}
}
