package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"codemypaper/internal/log"
)

// maxTestOutput is the byte cap on captured smoke-test output before it is fed
// back to the model or written to ERROR_LOG.md.
const maxTestOutput = 8 * 1024

// smokeTestFile is the one command the pipeline ever runs. The old per-model
// command allowlist is gone because the model no longer chooses commands.
const smokeTestFile = "smoke_test.py"

// testResult is the observed outcome of one smoke-test run.
type testResult struct {
	Output   string // combined stdout/stderr, capped
	ExitCode int    // -1 on timeout or start failure
	TimedOut bool
	Duration time.Duration
}

// passed reports whether the run counts as a green smoke-test.
func (r testResult) passed() bool { return r.ExitCode == 0 && !r.TimedOut }

// runSmokeTest executes `python3 smoke_test.py` in dir with a timeout and an
// output cap. All failure modes (non-zero exit, timeout, python3 missing) are
// folded into the result — the caller's verdict logic stays a single check.
func runSmokeTest(ctx context.Context, dir string, timeout time.Duration, logger *log.Logger) testResult {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(runCtx, "python3", smokeTestFile)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()
	res := testResult{
		Output:   capOutput(string(out), maxTestOutput),
		Duration: time.Since(start).Round(time.Millisecond),
	}

	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = -1
		res.Output += fmt.Sprintf("\n[smoke test timed out after %s]", timeout)
		logger.Debugf("smoke test timed out after %s", timeout)
		return res
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else {
			// e.g. python3 not on PATH: no exit code exists, surface the error text
			res.ExitCode = -1
			res.Output = strings.TrimSpace(res.Output + "\n" + runErr.Error())
		}
	}
	logger.Debugf("smoke test exited %d in %s", res.ExitCode, res.Duration)
	return res
}

// capOutput truncates s to max bytes, appending a notice of how much was omitted.
func capOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	omitted := len(s) - max
	head := strings.ToValidUTF8(s[:max], "")
	return head + fmt.Sprintf("\n... [output truncated: %d bytes omitted]", omitted)
}
