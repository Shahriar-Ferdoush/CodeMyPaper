package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Result struct {
	Output   string
	IsError  bool
	ExitCode int
}

type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, args map[string]any) (Result, error)
}

type Registry struct{ tools map[string]Tool }

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(t Tool) { r.tools[t.Name()] = t }

func (r *Registry) Run(ctx context.Context, name string, args map[string]any) (Result, error) {
	t, ok := r.tools[name]
	if !ok {
		return Result{IsError: true, Output: "unknown tool: " + name}, nil
	}
	return t.Run(ctx, args)
}

func (r *Registry) Descriptions() string {
	// Sort by name so the system prompt is deterministic across runs
	// (map iteration order is randomized).
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		t := r.tools[name]
		fmt.Fprintf(&b, "- %s: %s\n", t.Name(), t.Description())
	}
	return b.String()
}
