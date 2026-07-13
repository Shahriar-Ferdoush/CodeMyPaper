package llm

import "context"

// Role is a chat message's role.
type Role string

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

// The single seam to any chat backend.
type LLMClient interface {
	// Sends the conversation so far and returns the reply text.
	Chat(ctx context.Context, messages []Message) (string, error)
	// A human-readable identifier for the backend, used in logs.
	Name() string
}
