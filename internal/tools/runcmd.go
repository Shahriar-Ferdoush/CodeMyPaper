package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"codemypaper/internal/log"
)

// allowedCommands is the executable allowlist run_command enforces.
var allowedCommands = map[string]bool{
	"python": true, "python3": true,
	"pip": true, "pip3": true,
	"ls": true, "cat": true, "pytest": true,
}

// RunCommand runs one allowlisted command in the jail base directory, with a timeout.
type RunCommand struct {
	base    string
	timeout time.Duration
	log     *log.Logger
}

// NewRunCommand creates a run_command tool jailed to base with a per-command timeout.
func NewRunCommand(base string, timeout time.Duration, logger *log.Logger) *RunCommand {
	return &RunCommand{base: base, timeout: timeout, log: logger}
}

func (c *RunCommand) Name() string { return "run_command" }

func (c *RunCommand) Description() string {
	return `run_command: run one allowlisted command (python(3), pip(3), ls, cat, pytest) ` +
		`in the output directory. args: {"cmd": string}`
}

// Run executes args["cmd"] in the jail base directory if its executable is allowlisted.
// Input:
//   - ctx: context.Context; combined with the tool's configured timeout
//   - args: {"cmd": string}
//
// Output:
//   - Result: combined stdout/stderr (capped) and exit code; IsError set on a
//     disallowed command, timeout, or non-zero exit
func (c *RunCommand) Run(ctx context.Context, args map[string]any) (Result, error) {
	cmdStr, err := argString(args, "cmd")
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}

	fields := strings.Fields(cmdStr)
	if len(fields) == 0 {
		return Result{Output: "empty command", IsError: true}, nil
	}
	exe := fields[0]
	if !allowedCommands[exe] {
		return Result{
			Output:  fmt.Sprintf("command %q is not allowed; allowed: python(3), pip(3), ls, cat, pytest", exe),
			IsError: true,
		}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, exe, fields[1:]...)
	cmd.Dir = c.base

	out, runErr := cmd.CombinedOutput()
	capped := capOutput(string(out), defaultMaxOutput)

	if runCtx.Err() == context.DeadlineExceeded {
		c.log.Debugf("run_command %q timed out after %s", cmdStr, c.timeout)
		return Result{
			Output:   capped + fmt.Sprintf("\n[command timed out after %s]", c.timeout),
			IsError:  true,
			ExitCode: -1,
		}, nil
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			c.log.Debugf("run_command %q exited %d", cmdStr, exitErr.ExitCode())
			return Result{Output: capped, IsError: true, ExitCode: exitErr.ExitCode()}, nil
		}
		return Result{Output: capped + "\n" + runErr.Error(), IsError: true}, nil
	}

	c.log.Debugf("run_command %q exited 0", cmdStr)
	return Result{Output: capped, ExitCode: 0}, nil
}
