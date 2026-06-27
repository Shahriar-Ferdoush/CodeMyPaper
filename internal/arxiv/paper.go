package arxiv

import (
	"strings"
)

// Paper is the method-focused, char-budgeted view of an arXiv paper that the
// agent prompt is built from. Figures/FigureNotes are populated by the vision
// pre-pass (M3.5) and are empty in M3.
type Paper struct {
	ID, Title, Abstract string
	Sections            []Section
	Figures             []Figure
	FigureNotes         []FigureNote // filled by vision; may be empty
	Source              string       // "ar5iv" | "eprint"

	// maxChars is the context char budget set by Fetch and consumed by
	// PromptText. Unexported per DESIGN §3.3 (PromptText takes no argument).
	maxChars int
}

type Section struct {
	Heading, Body  string
	MethodRelevant bool
}

type Figure struct {
	Ref, Caption, ImageURL, Section string
	MethodRelevant                  bool
}

type FigureNote struct {
	Ref, Description string
}

// methodKeywords are matched (case-insensitive substring) against a section
// heading to flag it as method-relevant.
var methodKeywords = []string{
	"method", "approach", "model", "architecture", "algorithm",
	"framework", "training", "objective", "loss", "formulation", "proposed",
}

// isMethodRelevant reports whether a heading names a method-relevant section.
func isMethodRelevant(heading string) bool {
	h := strings.ToLower(heading)
	for _, kw := range methodKeywords {
		if strings.Contains(h, kw) {
			return true
		}
	}
	return false
}

const truncationNotice = "\n[... truncated to fit context budget ...]\n"

// promptSections selects which sections to render: the method-relevant ones,
// falling back to ALL sections when none were flagged. The fallback prevents
// silent context loss — without it, an ar5iv paper whose headings don't match
// any method keyword would hand the model only the title and abstract.
func (p *Paper) promptSections() []Section {
	var method []Section
	for _, s := range p.Sections {
		if s.MethodRelevant {
			method = append(method, s)
		}
	}
	if len(method) > 0 {
		return method
	}
	return p.Sections
}

// PromptText builds a clearly-delimited prompt body: title and abstract first,
// then the method sections that fit within the char budget (p.maxChars), then a
// figure-notes block when present. It returns the text and whether the body was
// truncated to fit the budget (FR2). It is a pure read — no side effects.
//
// The budget contract is honored strictly: the remaining budget is computed
// from the header (title+abstract) before iterating sections. A section that
// would overflow has the fitting prefix included (or is dropped) and a notice
// is emitted.
func (p *Paper) PromptText() (text string, truncated bool) {
	var b strings.Builder

	b.WriteString("TITLE: ")
	b.WriteString(strings.TrimSpace(p.Title))
	b.WriteString("\n\nABSTRACT:\n")
	b.WriteString(strings.TrimSpace(p.Abstract))
	b.WriteString("\n")

	header := b.String()
	remaining := p.maxChars - len(header)

	if remaining <= 0 {
		b.WriteString(truncationNotice)
		return b.String() + p.figureNotesBlock(), true
	}

	for _, s := range p.promptSections() {
		// Render the section into its own buffer so we can measure it.
		var sb strings.Builder
		sb.WriteString("\n## ")
		sb.WriteString(strings.TrimSpace(s.Heading))
		sb.WriteString("\n")
		sb.WriteString(strings.TrimSpace(s.Body))
		sb.WriteString("\n")
		chunk := sb.String()

		if len(chunk) <= remaining {
			b.WriteString(chunk)
			remaining -= len(chunk)
			continue
		}

		// Won't fit. Include the prefix that fits (if it's worth it) and stop.
		if remaining > len(truncationNotice) {
			b.WriteString(chunk[:remaining-len(truncationNotice)])
		}
		b.WriteString(truncationNotice)
		return b.String() + p.figureNotesBlock(), true
	}

	return b.String() + p.figureNotesBlock(), false
}

// figureNotesBlock renders the vision figure notes, or "" when there are none
// (the M3 case).
func (p *Paper) figureNotesBlock() string {
	if len(p.FigureNotes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nFIGURE NOTES:\n")
	for _, fn := range p.FigureNotes {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(fn.Ref))
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(fn.Description))
		b.WriteString("\n")
	}
	return b.String()
}
