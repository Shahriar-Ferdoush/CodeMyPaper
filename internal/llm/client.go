package llm

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ErrBackendUnreachable tags a transport-level failure to reach a chat backend
// (connection refused, DNS, hung dial). main maps it to exit code 3
// (PROJECT_SPECIFICATION §4: "fatal — backend unreachable"). It is matchable via
// errors.Is whether returned directly or wrapped with %w.
var ErrBackendUnreachable = errors.New("chat backend unreachable")

// requestTimeout bounds a single chat HTTP call so a hung connection can't block
// forever even when --wall-budget is 0 (unlimited). The per-run wall-budget
// context still bounds the overall run on top of this.
const requestTimeout = 5 * time.Minute

// httpClient is the shared client for all chat backends in this package.
var httpClient = &http.Client{Timeout: requestTimeout}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool      Role = "tool"
	// RoleFunction  Role = "function"
)

type Message struct {
	Role    Role
	Content string
}

type LLMClient interface {
	Chat(ctx context.Context, messages []Message) (string, error)
	Name() string
}
