package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type Ollama struct {
	Host  string
	Model string
}

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

func (o *Ollama) Name() string {
	return "ollama:" + o.Model
}

// Wire types - How Ollama's /api/chat wants JSON.
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

// Chat function - LLMClient interface implementation for Ollama.
// Sends a chat request to the Ollama server and returns the assistant's reply.
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
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: ollama (is the server running?): %v", ErrBackendUnreachable, err)
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
