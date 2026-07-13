package pipeline

import (
	"fmt"
	"strings"
)

const (
	fileMarkerPrefix = "=== FILE: "
	fileMarkerSuffix = " ==="
	endMarker        = "=== END FILE ==="
	methodPrefix     = "METHOD:"
)

// File is one named file extracted from a model reply. Files is a slice, not a
// map: it keeps write order deterministic and makes duplicate names visible.
type File struct {
	Name    string
	Content string
}

// Reply is the parsed form of one model reply.
type Reply struct {
	Method string
	Files  []File
}

// parseReply extracts the METHOD line and the file blocks from one raw model
// reply. Pure string → value: path jailing and reserved-name checks happen at
// write time, where outDir context lives. A non-nil error means the reply is
// malformed and its message is written for the model to read as the corrective
// re-prompt.
func parseReply(raw string) (Reply, error) {
	lines := strings.Split(stripOuterFence(raw), "\n")

	var reply Reply
	var name string      // file block currently open, "" = outside
	var content []string // accumulated block lines; joined on close, not +='d (O(n²))
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")

		if name == "" {
			switch {
			case isFileMarker(line):
				n := strings.TrimSpace(line[len(fileMarkerPrefix) : len(line)-len(fileMarkerSuffix)])
				if n == "" {
					return Reply{}, fmt.Errorf("file marker with empty name: %q", line)
				}
				name, content = n, nil
			case reply.Method == "" && strings.HasPrefix(line, methodPrefix):
				reply.Method = strings.TrimSpace(strings.TrimPrefix(line, methodPrefix))
			}
			// anything else outside a block is ignorable prose
			continue
		}

		// Inside a block only the exact end marker, alone on its line, closes it;
		// a line merely containing the marker text is file content.
		if line == endMarker {
			reply.Files = append(reply.Files, File{Name: name, Content: strings.Join(content, "\n") + "\n"})
			name = ""
			continue
		}
		content = append(content, line)
	}

	if name != "" {
		return Reply{}, fmt.Errorf("file block %q was never closed with %q", name, endMarker)
	}
	if len(reply.Files) == 0 {
		return Reply{}, fmt.Errorf("no %q file blocks found", fileMarkerPrefix+"<name>"+fileMarkerSuffix)
	}
	return reply, nil
}

// isFileMarker reports whether line is exactly an opening file marker.
func isFileMarker(line string) bool {
	return strings.HasPrefix(line, fileMarkerPrefix) &&
		strings.HasSuffix(line, fileMarkerSuffix) &&
		len(line) > len(fileMarkerPrefix)+len(fileMarkerSuffix)
}

// stripOuterFence removes one markdown code fence wrapping the whole reply —
// models add one even when told not to.
func stripOuterFence(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "```") {
		return raw
	}
	first := strings.IndexByte(s, '\n')
	if first < 0 || !strings.HasSuffix(s, "\n```") {
		return raw
	}
	return s[first+1 : len(s)-len("```")]
}
