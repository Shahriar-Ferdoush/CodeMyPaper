package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// ToolCall is a parsed request from the model: which tool to run, with what args.
type ToolCall struct {
	Name string
	Args map[string]any
}

var jsonBlock = regexp.MustCompile("(?s)```json\\s*(.*?)```")

// parseToolCall extracts the model's tool call from its raw reply.
// Input:
//   - raw: the model's full reply text
//
// Output:
//   - ToolCall: parsed from the LAST ```json block in raw
//   - error: if no block is found, it isn't valid JSON, or "tool" is missing
func parseToolCall(raw string) (ToolCall, error) {
	matches := jsonBlock.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return ToolCall{}, fmt.Errorf("no ```json tool-call block found in reply")
	}
	last := matches[len(matches)-1][1]

	var payload struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal([]byte(last), &payload); err != nil {
		return ToolCall{}, fmt.Errorf("tool-call block is not valid JSON: %w", err)
	}
	if payload.Tool == "" {
		return ToolCall{}, fmt.Errorf(`tool-call block is missing the required "tool" field`)
	}
	return ToolCall{Name: payload.Tool, Args: payload.Args}, nil
}
