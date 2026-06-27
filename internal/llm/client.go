package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type LLMClient interface {
	Chat(ctx context.Context, messages []Message) (string, error)
	Name() string
}
