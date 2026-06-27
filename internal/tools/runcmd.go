package tools

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type RunCommand struct {
	BaseDir string
	Timeout time.Duration
}

var allowed = map[string]bool{
	"python": true, "python3": true, "pip": true, "pip3": true,
	"ls": true, "cat": true, "pytest": true,
}

func (r RunCommand) Name() string { return "run_command" }
func (r RunCommand) Description() string {
	return "run_command(cmd): run an allowlisted command in the project dir"
}

func (r RunCommand) Run(ctx context.Context, args map[string]any) (Result, error) {
	cmdStr, _ := args["cmd"].(string)
	fields := strings.Fields(cmdStr) // NOTE: naive split (no quotes) — fine for v1
	if len(fields) == 0 {
		return Result{IsError: true, Output: "empty command"}, nil
	}
	if !allowed[fields[0]] {
		return Result{IsError: true, Output: "command not allowed: " + fields[0]}, nil
	}
	// cwd-jail (NFR1): reject any argument that is an absolute path or escapes
	// the project dir via "..". Non-path args (flags, package names) join
	// harmlessly inside the base and pass. This is a normal observation the
	// model can recover from, not a fatal error.
	for _, arg := range fields[1:] {
		if _, err := safeJoin(r.BaseDir, arg); err != nil {
			return Result{IsError: true, Output: err.Error()}, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	c := exec.CommandContext(ctx, fields[0], fields[1:]...)
	c.Dir = r.BaseDir
	out, err := c.CombinedOutput()
	output := capOutput(string(out), maxOutputBytes)

	if ctx.Err() == context.DeadlineExceeded {
		return Result{IsError: true, Output: output + "\n[timed out]", ExitCode: -1}, nil
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return Result{Output: output, ExitCode: ee.ExitCode()}, nil // program ran, returned nonzero
		}
		return Result{IsError: true, Output: err.Error()}, nil // couldn't start
	}
	return Result{Output: output, ExitCode: 0}, nil
}
