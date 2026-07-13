package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ErrNoAPIKey, ErrBackendUnreachable, ErrRateLimited are typed failures the CLI maps to
// exit codes: ErrNoAPIKey → 2 (config), the others → 3.
var (
	ErrNoAPIKey           = errors.New("gemini: GEMINI_API_KEY is not set")
	ErrBackendUnreachable = errors.New("gemini: backend unreachable")
	ErrRateLimited        = errors.New("gemini: rate limited")
)

const defaultGeminiBase = "https://generativelanguage.googleapis.com/v1beta"

// Gemini is an LLMClient backed by the Generative Language API's generateContent
// endpoint. Gemini has no system role, so system text is folded into the first user turn.
type Gemini struct {
	apiKey  string
	Model   string
	baseURL string // overridable in tests; defaults to the live endpoint
	client  *http.Client
}

// Creates a Gemini client for the given hosted model id, reading the API key
// from GEMINI_API_KEY.
func NewGemini(model string) *Gemini {
	return &Gemini{
		apiKey:  os.Getenv("GEMINI_API_KEY"),
		Model:   model,
		baseURL: defaultGeminiBase,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// Returns a human-readable identifier for this backend, used in logs.
func (g *Gemini) Name() string { return "Gemini: " + g.Model }

// geminiPart, geminiContent, geminiReq, geminiResp are the JSON shapes the
// generateContent endpoint expects and returns.
type geminiPart struct {
	Text string `json:"text"`
}
type geminiContent struct {
	Role  string       `json:"role,omitempty"` // "user" | "model"
	Parts []geminiPart `json:"parts"`
}
type geminiReq struct {
	Contents []geminiContent `json:"contents"`
}
type geminiResp struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Sends messages to the generateContent endpoint and returns the reply text.
// Input:
//   - ctx: context.Context for cancellation and deadlines
//   - messages: the conversation so far
//
// Output:
//   - string: the model's reply text
//   - error: ErrNoAPIKey, ErrRateLimited, ErrBackendUnreachable, or a wrapped decode/status error
func (g *Gemini) Chat(ctx context.Context, messages []Message) (string, error) {
	if g.apiKey == "" {
		return "", ErrNoAPIKey
	}

	body, err := json.Marshal(geminiReq{Contents: foldMessages(messages)})
	if err != nil {
		return "", fmt.Errorf("gemini: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent", g.baseURL, g.Model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gemini: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey) // key in header — never in URL or logs

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBackendUnreachable, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("gemini: read response: %w", err)
	}
	var out geminiResp
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("gemini: decode response (status %s): %w", resp.Status, err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return "", ErrRateLimited
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("%w (%s)", ErrNoAPIKey, errMessage(out)) // errMessage never contains the key
	default:
		return "", fmt.Errorf("gemini: unexpected status %s: %s", resp.Status, errMessage(out))
	}

	if out.PromptFeedback.BlockReason != "" {
		return "", fmt.Errorf("gemini: prompt blocked (%s)", out.PromptFeedback.BlockReason)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini: empty response (finishReason=%s)", finishReason(out))
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}

// Maps provider-neutral Messages to Gemini contents: accumulated system text
// is prepended to the next user turn (or becomes its own trailing user turn
// if none follows), and assistant turns map to role "model".
// Input:
//   - messages: the provider-neutral conversation
//
// Output:
//   - []geminiContent: the Gemini-shaped contents
func foldMessages(messages []Message) []geminiContent {
	var pendingSystem string
	var contents []geminiContent

	flushUser := func(text string) {
		if pendingSystem != "" {
			text = pendingSystem + "\n\n" + text
			pendingSystem = ""
		}
		contents = append(contents, geminiContent{Role: "user", Parts: []geminiPart{{Text: text}}})
	}

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			if pendingSystem != "" {
				pendingSystem += "\n\n"
			}
			pendingSystem += m.Content
		case RoleAssistant:
			contents = append(contents, geminiContent{Role: "model", Parts: []geminiPart{{Text: m.Content}}})
		default: // RoleUser
			flushUser(m.Content)
		}
	}
	if pendingSystem != "" {
		contents = append(contents, geminiContent{Role: "user", Parts: []geminiPart{{Text: pendingSystem}}})
	}
	return contents
}

// Returns the API's error message, or a placeholder if none was set.
func errMessage(r geminiResp) string {
	if r.Error != nil {
		return r.Error.Message
	}
	return "no error detail"
}

// Returns the first candidate's finish reason, or "none" if there were no candidates.
func finishReason(r geminiResp) string {
	if len(r.Candidates) > 0 {
		return r.Candidates[0].FinishReason
	}
	return "none"
}
