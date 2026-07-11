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

// LLMClient interface
// Chat(ctx, messages) -> sends the conversation so far and returns the reply text.
// Name() -> a human-readable identifier for the backend, used in logs.
type LLMClient interface {
	Chat(ctx context.Context, messages []Message) (string, error)
	Name() string
}
