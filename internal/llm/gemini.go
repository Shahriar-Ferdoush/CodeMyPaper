package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Sentinel errors. Both are matchable via errors.Is whether returned directly
// or wrapped with %w.
var (
	ErrNoAPIKey    = errors.New("GEMINI_API_KEY not set")
	ErrRateLimited = errors.New("gemini rate limited")
)

type Gemini struct {
	Host   string
	Model  string
	apiKey string
}

func NewGemini(model string) *Gemini {
	host := os.Getenv("GEMINI_HOST")
	if host == "" {
		host = "https://generativelanguage.googleapis.com"
	}
	return &Gemini{
		Host:   host,
		Model:  model,
		apiKey: os.Getenv("GEMINI_API_KEY"),
	}
}

func (g *Gemini) Name() string {
	return "gemini:" + g.Model
}

// Wire types - how the Generative Language API's generateContent wants JSON.
type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiReq struct {
	Contents []geminiContent `json:"contents"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiResp struct {
	Candidates []geminiCandidate `json:"candidates"`
}

// Chat implements LLMClient for the Gemini backend.
//
// Gemini has no system role, so all RoleSystem messages are folded: their
// contents are joined and prepended (separated by "\n\n") to the first user
// turn's text. RoleAssistant maps to "model" and RoleUser maps to "user".
func (g *Gemini) Chat(ctx context.Context, messages []Message) (string, error) {
	if g.apiKey == "" {
		return "", ErrNoAPIKey
	}

	// Gather all system message contents to fold into the first user turn.
	var systemParts []string
	for _, m := range messages {
		if m.Role == RoleSystem {
			systemParts = append(systemParts, m.Content)
		}
	}
	systemPrefix := strings.Join(systemParts, "\n\n")

	contents := make([]geminiContent, 0, len(messages))
	systemFolded := false
	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			// Handled via systemPrefix; skip here.
			continue
		case RoleAssistant:
			contents = append(contents, geminiContent{
				Role:  "model",
				Parts: []geminiPart{{Text: m.Content}},
			})
		default: // RoleUser (and any unknown role) maps to "user".
			text := m.Content
			if !systemFolded && systemPrefix != "" {
				text = systemPrefix + "\n\n" + text
				systemFolded = true
			}
			contents = append(contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: text}},
			})
		}
	}

	// Ensure the system text is delivered even if there was no user turn:
	// the first entry must be a "user" role carrying the system prefix.
	if !systemFolded && systemPrefix != "" {
		contents = append([]geminiContent{{
			Role:  "user",
			Parts: []geminiPart{{Text: systemPrefix}},
		}}, contents...)
	}

	body, err := json.Marshal(geminiReq{Contents: contents})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// The API key only appears in this URL. It must never end up in a logged
	// or returned string.
	url := g.Host + "/v1beta/models/" + g.Model + ":generateContent?key=" + g.apiKey

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		// Do not wrap err here: a request-construction error may embed the
		// URL (and thus the key). Report a key-free message instead.
		return "", errors.New("failed to create gemini request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		// Do not wrap the original err: transport errors from Do may embed the
		// URL (and thus the key). Wrap only the key-free sentinel so the failure
		// is still classifiable as "backend unreachable" (exit 3).
		return "", fmt.Errorf("%w: gemini request failed", ErrBackendUnreachable)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to parsing below
	case http.StatusTooManyRequests:
		return "", fmt.Errorf("%w (status 429)", ErrRateLimited)
	default:
		// Status code only - never the URL.
		return "", fmt.Errorf("gemini returned non-200 status code: %d", resp.StatusCode)
	}

	// Decode errors do not embed the key, so wrapping them is safe.
	var out geminiResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(out.Candidates) == 0 {
		return "", errors.New("gemini returned no candidates (possibly safety-blocked)")
	}
	parts := out.Candidates[0].Content.Parts
	if len(parts) == 0 {
		return "", errors.New("gemini returned an empty candidate (possibly safety-blocked)")
	}

	return parts[0].Text, nil
}
