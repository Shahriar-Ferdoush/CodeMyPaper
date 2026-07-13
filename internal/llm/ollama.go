package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// Ollama is an LLMClient backed by a local Ollama server's /api/chat endpoint.
type Ollama struct {
	Host  string
	Model string
}

// Creates an Ollama client for the given local model id, reading the host
// from OLLAMA_HOST (default http://localhost:11434).
func NewOllama(model string) *Ollama {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}
	return &Ollama{
		Host:  host,
		Model: model,
	}
}

// Returns a human-readable identifier for this backend, used in logs.
func (o *Ollama) Name() string {
	return "Ollama: " + o.Model
}

// ollamaMsg, ollamaReq, ollamaResp are the JSON shapes Ollama's /api/chat expects and returns.
type ollamaMsg struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type ollamaReq struct {
	Model    string      `json:"model"`
	Messages []ollamaMsg `json:"messages"`
	Stream   bool        `json:"stream"`
}

type ollamaResp struct {
	Message ollamaMsg `json:"message"`
}

// Sends messages to the Ollama server's /api/chat endpoint and returns the reply text.
// Input:
//   - ctx: context.Context for cancellation and deadlines
//   - messages: the conversation so far
//
// Output:
//   - string: the model's reply text
//   - error: on a marshal/request/decode failure or a non-200 response
func (o *Ollama) Chat(ctx context.Context, messages []Message) (string, error) {
	msgs := make([]ollamaMsg, len(messages))
	for i, m := range messages {
		msgs[i] = ollamaMsg{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	body, err := json.Marshal(ollamaReq{
		Model:    o.Model,
		Messages: msgs,
		Stream:   false,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Host+"/api/chat", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama unreachable (Is the Ollama server running?): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned non-200 status: %s", resp.Status)
	}

	var out ollamaResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return out.Message.Content, nil
}
