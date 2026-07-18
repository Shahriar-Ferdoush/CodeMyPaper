package arxiv

import (
	"strings"
	"unicode/utf8"
)

// Section is one heading-delimited chunk of the paper body.
type Section struct {
	Heading        string
	Body           string
	MethodRelevant bool
}

// Paper is the ingested, method-focused view of an arXiv paper.
type Paper struct {
	ID       string
	Title    string
	Abstract string
	Sections []Section
	Source   string // "arxiv-html" | "ar5iv" | "eprint" | "api-only"
	Raw      []byte // exact body of the winning source rung, as served (nil for api-only)
	RawName  string // filename for Raw: "paper.html" | "paper.tar.gz" | "paper.tex.gz"
}

// methodKeywords is the case-insensitive keyword set used to flag method-bearing sections.
var methodKeywords = []string{
	"method", "approach", "model", "architecture",
	"algorithm", "framework", "proposed", "technical",
}

// Reports whether heading contains a word from methodKeywords.
func isMethodRelevant(heading string) bool {
	h := strings.ToLower(heading)
	for _, kw := range methodKeywords {
		if strings.Contains(h, kw) {
			return true
		}
	}
	return false
}

// PromptText assembles the method-focused context handed to the model: title + abstract
// (always, as a backstop), followed by every MethodRelevant section.
// Input:
//   - maxChars: char budget to trim text to (0 = unlimited)
//
// Output:
//   - text: the rendered context
//   - truncated: whether maxChars cut content
func (p *Paper) PromptText(maxChars int) (text string, truncated bool) {
	var b strings.Builder
	if p.Title != "" {
		b.WriteString("# " + p.Title + "\n\n")
	}
	if p.Abstract != "" {
		b.WriteString("## Abstract\n" + p.Abstract + "\n\n")
	}
	for _, s := range p.Sections {
		if !s.MethodRelevant {
			continue
		}
		if s.Heading != "" {
			b.WriteString("## " + s.Heading + "\n")
		}
		if s.Body != "" {
			b.WriteString(s.Body + "\n\n")
		}
	}
	text = strings.TrimSpace(b.String())

	if maxChars > 0 && len(text) > maxChars {
		text = strings.TrimSpace(truncateAtRune(text, maxChars))
		truncated = true
	}
	return text, truncated
}

// Cuts s to at most maxBytes bytes without splitting a multi-byte rune.
// Input:
//   - s: the string to cut
//   - maxBytes: the byte limit
//
// Output:
//   - string: s truncated at the nearest rune boundary at or before maxBytes
func truncateAtRune(s string, maxBytes int) string {
	if maxBytes >= len(s) {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
