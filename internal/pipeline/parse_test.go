package pipeline

import "testing"

func TestParseReply(t *testing.T) {
	// Testing - a well-formed reply with a METHOD line and one file block
	// Input - "METHOD: attention\n=== FILE: model.py ===\nprint(1)\n=== END FILE ==="
	// Expected output - Method set, one file with a trailing newline on its content
	t.Run("well-formed single file", func(t *testing.T) {
		raw := "METHOD: attention\n" +
			fileMarkerPrefix + "model.py" + fileMarkerSuffix + "\n" +
			"print(1)\n" +
			endMarker
		reply, err := parseReply(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if reply.Method != "attention" {
			t.Fatalf("Method = %q, want %q", reply.Method, "attention")
		}
		if len(reply.Files) != 1 || reply.Files[0].Name != "model.py" {
			t.Fatalf("Files = %+v, want one file named model.py", reply.Files)
		}
		if reply.Files[0].Content != "print(1)\n" {
			t.Fatalf("Content = %q, want %q", reply.Files[0].Content, "print(1)\n")
		}
	})

	// Testing - a reply with several file blocks
	// Input - two "=== FILE ===" blocks
	// Expected output - both files parsed in order
	t.Run("multiple files", func(t *testing.T) {
		raw := fileMarkerPrefix + "model.py" + fileMarkerSuffix + "\n" +
			"a\n" + endMarker + "\n" +
			fileMarkerPrefix + "main.py" + fileMarkerSuffix + "\n" +
			"b\n" + endMarker
		reply, err := parseReply(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(reply.Files) != 2 || reply.Files[0].Name != "model.py" || reply.Files[1].Name != "main.py" {
			t.Fatalf("Files = %+v, want [model.py main.py] in order", reply.Files)
		}
	})

	// Testing - a reply wrapped in an outer markdown fence
	// Input - "```\n<file block>\n```"
	// Expected output - fence stripped, file block still parses
	t.Run("outer fence stripped", func(t *testing.T) {
		inner := fileMarkerPrefix + "model.py" + fileMarkerSuffix + "\n" +
			"print(1)\n" + endMarker
		raw := "```\n" + inner + "\n```"
		reply, err := parseReply(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(reply.Files) != 1 || reply.Files[0].Name != "model.py" {
			t.Fatalf("Files = %+v, want one file named model.py", reply.Files)
		}
	})

	// Testing - a line inside a block that only contains marker-like text, not whole-line-exact
	// Input - a block whose content is "print(=== END FILE ===)"
	// Expected output - treated as content, block stays open until the real end marker
	t.Run("marker-like text inside block is content", func(t *testing.T) {
		raw := fileMarkerPrefix + "model.py" + fileMarkerSuffix + "\n" +
			"print(" + endMarker + ")\n" +
			endMarker
		reply, err := parseReply(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "print(" + endMarker + ")\n"
		if reply.Files[0].Content != want {
			t.Fatalf("Content = %q, want %q", reply.Files[0].Content, want)
		}
	})

	// Testing - a file block with no closing end marker
	// Input - "=== FILE: model.py ===\nprint(1)"
	// Expected output - error
	t.Run("unclosed block", func(t *testing.T) {
		raw := fileMarkerPrefix + "model.py" + fileMarkerSuffix + "\nprint(1)"
		if _, err := parseReply(raw); err == nil {
			t.Fatal("expected error for unclosed block, got nil")
		}
	})

	// Testing - a reply with no file blocks at all
	// Input - "METHOD: attention\nsome prose"
	// Expected output - error
	t.Run("no file blocks", func(t *testing.T) {
		raw := "METHOD: attention\nsome prose"
		if _, err := parseReply(raw); err == nil {
			t.Fatal("expected error for no file blocks, got nil")
		}
	})

	// Testing - a file marker whose name is empty after trimming
	// Input - "=== FILE:   ===" (built from the constants themselves)
	// Expected output - error
	t.Run("empty file name", func(t *testing.T) {
		raw := fileMarkerPrefix + "  " + fileMarkerSuffix + "\n" +
			"x\n" + endMarker
		if _, err := parseReply(raw); err == nil {
			t.Fatal("expected error for empty file name, got nil")
		}
	})

	// Testing - two METHOD lines in the same reply
	// Input - "METHOD: first\nMETHOD: second\n<file block>"
	// Expected output - only the first METHOD line is kept
	t.Run("first METHOD line wins", func(t *testing.T) {
		raw := "METHOD: first\nMETHOD: second\n" +
			fileMarkerPrefix + "model.py" + fileMarkerSuffix + "\n" +
			"x\n" + endMarker
		reply, err := parseReply(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if reply.Method != "first" {
			t.Fatalf("Method = %q, want %q", reply.Method, "first")
		}
	})

	// Testing - CRLF line endings throughout the reply
	// Input - the well-formed single-file case with \r\n instead of \n
	// Expected output - parses identically to the LF version
	t.Run("CRLF line endings", func(t *testing.T) {
		raw := "METHOD: attention\r\n" +
			fileMarkerPrefix + "model.py" + fileMarkerSuffix + "\r\n" +
			"print(1)\r\n" +
			endMarker + "\r\n"
		reply, err := parseReply(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if reply.Method != "attention" {
			t.Fatalf("Method = %q, want %q", reply.Method, "attention")
		}
		if len(reply.Files) != 1 || reply.Files[0].Content != "print(1)\n" {
			t.Fatalf("Files = %+v, want one file with content %q", reply.Files, "print(1)\n")
		}
	})
}

func TestIsFileMarker(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{
			// Testing - a valid marker with a name
			// Input - "=== FILE: model.py ==="
			// Expected output - true
			name: "valid marker",
			line: fileMarkerPrefix + "model.py" + fileMarkerSuffix,
			want: true,
		},
		{
			// Testing - a line missing the marker prefix
			// Input - "FILE: model.py ==="
			// Expected output - false
			name: "missing prefix",
			line: "FILE: model.py" + fileMarkerSuffix,
			want: false,
		},
		{
			// Testing - a line missing the marker suffix
			// Input - "=== FILE: model.py"
			// Expected output - false
			name: "missing suffix",
			line: fileMarkerPrefix + "model.py",
			want: false,
		},
		{
			// Testing - prefix and suffix with no name between them
			// Input - "=== FILE:  ===" (prefix+suffix concatenated, no room for a name)
			// Expected output - false
			name: "no room for a name",
			line: fileMarkerPrefix + fileMarkerSuffix,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFileMarker(tc.line); got != tc.want {
				t.Fatalf("isFileMarker(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestStripOuterFence(t *testing.T) {
	// Testing - a reply wrapped in a single outer fence
	// Input - "```\ncontent\n```"
	// Expected output - fence markers removed, "content" left
	t.Run("fenced input", func(t *testing.T) {
		got := stripOuterFence("```\ncontent\n```")
		if got != "content\n" {
			t.Fatalf("stripOuterFence = %q, want %q", got, "content\n")
		}
	})

	// Testing - a reply with no fence at all
	// Input - "plain text"
	// Expected output - returned unchanged
	t.Run("no fence", func(t *testing.T) {
		raw := "plain text"
		got := stripOuterFence(raw)
		if got != raw {
			t.Fatalf("stripOuterFence = %q, want unchanged %q", got, raw)
		}
	})

	// Testing - input that starts with a fence but never closes it
	// Input - "```\ncontent without a closing fence"
	// Expected output - returned unchanged (original raw)
	t.Run("unclosed fence", func(t *testing.T) {
		raw := "```\ncontent without a closing fence"
		got := stripOuterFence(raw)
		if got != raw {
			t.Fatalf("stripOuterFence = %q, want unchanged %q", got, raw)
		}
	})
}
