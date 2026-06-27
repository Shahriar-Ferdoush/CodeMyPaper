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

type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, args map[string]any) (Result, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		panic(fmt.Sprintf("tools: duplicate registration for %q", name))
	}
	r.tools[name] = t
}

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

func (r *Registry) Tools() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
