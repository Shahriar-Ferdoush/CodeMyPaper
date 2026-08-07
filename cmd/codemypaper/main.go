// Command codemypaper turns an arXiv paper into a runnable Python/PyTorch
// reference implementation via a fixed two-call LLM pipeline.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shahriar-ferdoush/codemypaper/internal/arxiv"
	"github.com/shahriar-ferdoush/codemypaper/internal/llm"
	"github.com/shahriar-ferdoush/codemypaper/internal/log"
	"github.com/shahriar-ferdoush/codemypaper/internal/pipeline"
	"github.com/spf13/cobra"
)

var version = "dev"

// exitError carries a process exit code up to main alongside the underlying error.
type exitError struct {
	code int
	err  error
}

// Returns the underlying error's message, or "" if err is nil.
func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

// Wraps err with the process exit code it should map to.
func exitErr(code int, err error) error { return &exitError{code: code, err: err} }

// Builds the `version` subcommand.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of CodeMyPaper",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(version)
		},
	}
}

// Builds the `run` subcommand: fetch the arXiv paper and drive the fixed
// two-call pipeline (generate → smoke-test → one repair → smoke-test).
func runCmd() *cobra.Command {
	var (
		model           string
		geminiModel     string
		ollamaModel     string
		outDir          string
		testTimeout     time.Duration
		maxContextChars int
		verbose         bool
		refetch         bool
	)
	cmd := &cobra.Command{
		Use:   "run <arxiv-id-or-url>",
		Short: "Generate an implementation of an arXiv paper",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := log.New(os.Stderr, verbose)
			ctx := cmd.Context()

			client, err := buildClient(model, geminiModel, ollamaModel)
			if err != nil {
				return err
			}

			id, err := arxiv.ParseID(args[0])
			if err != nil {
				return exitErr(2, fmt.Errorf("resolve paper: %w", err))
			}
			if outDir == "" {
				outDir = filepath.Join("out", id)
			}
			if err := os.MkdirAll(outDir, 0o750); err != nil {
				return exitErr(3, fmt.Errorf("create out dir: %w", err))
			}

			// Persist the run's log next to its other artifacts.
			// A failure here defeats the file record but must never fail the run itself.
			if err := logger.AttachFile(filepath.Join(outDir, "run.log")); err != nil {
				logger.Errorf("could not attach log file: %v", err)
			}
			defer logger.Close() //nolint:errcheck // run is over when this fires; nowhere to report a close failure

			paper, err := loadOrFetchPaper(ctx, logger, outDir, id, args[0], refetch)
			if err != nil {
				if errors.Is(err, arxiv.ErrSourcesExhausted) {
					return exitErr(3, fmt.Errorf("fetch paper: %w", err))
				}
				return exitErr(2, fmt.Errorf("resolve paper: %w", err))
			}
			logger.Infof("using %q via %s (%d sections)",
				paper.Title, paper.Source, len(paper.Sections))

			logger.Infof("backend=%s out=%s", client.Name(), outDir)

			cfg := pipeline.Config{OutDir: outDir, TestTimeout: testTimeout, MaxContextChars: maxContextChars}
			outcome, err := pipeline.Run(ctx, client, paper, cfg, logger)
			if err != nil {
				// Cobra prints the error to stderr only; record it in run.log too.
				// A failed run is exactly when the file record matters most.
				logger.Errorf("run failed: %v", err)
				return classifyRunError(err)
			}

			fmt.Printf("\noutcome: %s (success=%v)\n", outcome.StopReason, outcome.Success)
			if outcome.Method != "" {
				fmt.Println("method:", outcome.Method)
			}
			if !outcome.Success {
				return exitErr(1, nil)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&model, "model", "gemini", "backend: gemini or ollama")
	cmd.Flags().StringVar(&geminiModel, "gemini-model", "gemini-3.1-flash-lite", "hosted model id (gemini backend)")
	cmd.Flags().StringVar(&ollamaModel, "ollama-model", "qwen2.5-coder:3b", "local model id (ollama backend)")
	cmd.Flags().StringVar(&outDir, "out", "", "output directory; defaults to ./out/<arxiv-id>")
	cmd.Flags().DurationVar(&testTimeout, "timeout", 120*time.Second, "smoke-test timeout")
	cmd.Flags().IntVar(&maxContextChars, "max-context-chars", 60000, "paper context budget in characters")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream pipeline debug logging to stderr")
	cmd.Flags().BoolVar(&refetch, "refetch", false, "force a fresh network fetch, bypassing any cached paper source")

	return cmd
}

// rawNames enumerates every raw-source filename a fetch rung might produce, so a fresh
// fetch can clear out a stale file left by a previous run that landed on a different rung.
var rawNames = []string{"paper.html", "paper.tar.gz", "paper.tex.gz"}

// cacheMetaName is the sidecar written next to a fetched paper's raw source, letting a
// later run rebuild the same Paper with no network call.
const cacheMetaName = "paper.meta.json"

// Returns a cache-hit Paper reconstructed entirely from outDir, or falls back
// to a fresh arxiv.Fetch (network) on a cache miss, a corrupt cache, or
// --refetch. A fresh fetch's result is persisted for the next rerun via persistPaper.
// Input:
//   - outDir: the run's output directory (already created)
//   - id: the canonical arXiv id (already parsed from idOrURL)
//   - idOrURL: the raw CLI argument, passed through to arxiv.Fetch on a cache miss
//   - refetch: --refetch — skip the cache unconditionally
//
// Output:
//   - *arxiv.Paper: the loaded or freshly-fetched paper
//   - error: arxiv.Fetch's error on a cache miss/refetch, untouched (callers classify it)
func loadOrFetchPaper(ctx context.Context, logger *log.Logger, outDir, id, idOrURL string, refetch bool) (*arxiv.Paper, error) {
	if !refetch {
		if paper, ok := loadCachedPaper(logger, outDir, id); ok {
			return paper, nil
		}
	}

	logger.Infof("fetching arXiv:%s ...", idOrURL)
	paper, err := arxiv.Fetch(ctx, idOrURL)
	if err != nil {
		return nil, err
	}
	persistPaper(logger, outDir, paper)
	return paper, nil
}

// Attempts a fully offline rebuild from outDir/paper.meta.json plus the raw
// source it names. Any problem — no sidecar yet, a corrupt one, or a
// missing/unparsable raw file — is logged (except the ordinary first-run
// case) and treated as a cache miss, never a hard error: a bad cache must not
// break a run that would otherwise succeed.
// Input:
//   - logger: run logger
//   - outDir: the run's output directory
//   - id: the canonical arXiv id
//
// Output:
//   - *arxiv.Paper: the rebuilt paper, on a cache hit
//   - bool: whether the cache hit
func loadCachedPaper(logger *log.Logger, outDir, id string) (*arxiv.Paper, bool) {
	metaPath := filepath.Join(outDir, cacheMetaName)
	metaBytes, err := os.ReadFile(metaPath) // #nosec G304 -- path is our own sidecar under the out dir, not model/user input
	if err != nil {
		return nil, false // no cache yet — the common case, not worth logging
	}

	var meta arxiv.CachedMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		logger.Warnf("cache: corrupt %s, fetching fresh: %v", metaPath, err)
		return nil, false
	}

	raw, err := os.ReadFile(filepath.Join(outDir, meta.RawName)) // #nosec G304 -- filename comes from our own sidecar, confined to the out dir
	if err != nil {
		logger.Warnf("cache: raw source %s missing, fetching fresh: %v", meta.RawName, err)
		return nil, false
	}

	paper, err := arxiv.FromCache(id, meta, raw)
	if err != nil {
		logger.Warnf("cache: %v, fetching fresh", err)
		return nil, false
	}

	logger.Infof("using cached paper source (%s) — pass --refetch to force a fresh fetch", metaPath)
	return paper, true
}

// Saves a freshly-fetched paper's raw source plus its cache sidecar, first
// removing any other rung's file left over from a previous run that landed
// differently. Best-effort throughout — a save failure must not fail a run
// that already has its Paper in hand.
// Input:
//   - logger: run logger
//   - outDir: the run's output directory
//   - paper: the freshly-fetched paper to persist
func persistPaper(logger *log.Logger, outDir string, paper *arxiv.Paper) {
	for _, name := range rawNames {
		if name == paper.RawName {
			continue
		}
		stale := filepath.Join(outDir, name)
		if err := os.Remove(stale); err == nil {
			logger.Infof("removed stale cached source %s", stale)
		}
	}

	if len(paper.Raw) == 0 {
		return // api-only: nothing to cache
	}

	rawPath := filepath.Join(outDir, paper.RawName)
	if err := os.WriteFile(rawPath, paper.Raw, 0o600); err != nil {
		logger.Warnf("could not save fetched source: %v", err)
		return
	}
	logger.Infof("saved fetched source to %s", rawPath)

	meta := arxiv.CachedMeta{
		Source:   paper.Source,
		RawName:  paper.RawName,
		Title:    paper.Title,
		Abstract: paper.Abstract,
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		logger.Warnf("could not encode cache metadata: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(outDir, cacheMetaName), metaBytes, 0o600); err != nil {
		logger.Warnf("could not save cache metadata: %v", err)
	}
}

// Selects the LLMClient backend from --model.
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

// Maps a pipeline.Run error to a process exit code.
// Input:
//   - err: the error returned by pipeline.Run
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

// Runs the CLI and maps the returned error to a process exit code (0 ok · 1 no
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
