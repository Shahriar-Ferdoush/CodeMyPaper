package tools

import (
	"context"
	"fmt"
	"sort"
)

type Result struct {
	Output   string
	IsError  bool
	ExitCode int
}

// Tool interface
// Name() -> the tool's name, used in the JSON "tool" field.
// Description() -> a short description of the tool, used in the system prompt.
// Run(ctx, args) -> executes the tool with the given arguments and returns a Result.
type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, args map[string]any) (Result, error)
}

// Registry of tools { name -> Tool }.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
// Panic -> if a tool with the same name is already registered.
func (r *Registry) Register(t Tool) {
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		panic(fmt.Sprintf("tools: duplicate registration for %q", name))
	}
	r.tools[name] = t
}

// Run executes the tool
// Input:
//   - ctx: context.Context for cancellation and deadlines
//   - name: the name of the tool to run
//   - args: a map of arguments to pass to the tool
//
// Output:
//   - Result: the result of the tool execution, including output, error status, and exit code
//   - error: any error that occurred during execution
func (r *Registry) Run(ctx context.Context, name string, args map[string]any) (Result, error) {
	t, ok := r.tools[name]
	if !ok {
		return Result{
			Output:  fmt.Sprintf("unknown tool: %q", name),
			IsError: true,
		}, nil
	}
	res, err := t.Run(ctx, args)
	if err != nil {
		return Result{Output: err.Error(), IsError: true}, nil
	}
	return res, nil
}

// Tools funtion to get all registered tools sorted by name
// Input:
//   - none
//
// Output:
//   - []Tool: a slice of all registered tools, sorted by name
func (r *Registry) Tools() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
