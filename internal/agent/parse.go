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
