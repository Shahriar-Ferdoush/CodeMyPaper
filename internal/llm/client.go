// Package llm defines the provider-neutral chat seam (LLMClient) and its
// backend implementations (Ollama, Gemini).
package llm

import "context"

// Role is a chat message's role.
type Role string

// The three chat roles every backend understands.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn in a chat conversation.
type Message struct {
	Role    Role
	Content string
}

// LLMClient is the single seam to any chat backend.
//
//nolint:revive // "llm.LLMClient" stutters, but the name is the documented seam across DESIGN.md/README; renaming buys nothing
type LLMClient interface {
	// Sends the conversation so far and returns the reply text.
	Chat(ctx context.Context, messages []Message) (string, error)
	// A human-readable identifier for the backend, used in logs.
	Name() string
}
