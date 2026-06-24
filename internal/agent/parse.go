package agent

import (
	"encoding/json"
	"errors"
	"regexp"
)

type toolCall struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

var jsonBlock = regexp.MustCompile("(?s)```json\\s*(.*?)```")

// parseToolCall extracts the LAST ```json {...}``` block and unmarshals it.
func parseToolCall(raw string) (toolCall, error) {
	m := jsonBlock.FindAllStringSubmatch(raw, -1)
	if len(m) == 0 {
		return toolCall{}, errors.New("no ```json tool block found")
	}
	var tc toolCall
	if err := json.Unmarshal([]byte(m[len(m)-1][1]), &tc); err != nil {
		return toolCall{}, err
	}
	if tc.Tool == "" {
		return toolCall{}, errors.New(`missing "tool" field`)
	}
	return tc, nil
}
