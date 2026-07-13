package arxiv

import (
	"html"
	"regexp"
	"strings"
)

// headingRe, anyTagRe, scriptRe, styleRe strip LaTeXML HTML down to heading-delimited text.
var (
	headingRe = regexp.MustCompile(`(?is)<h[1-6][^>]*>(.*?)</h[1-6]>`)
	anyTagRe  = regexp.MustCompile(`(?s)<[^>]+>`)
	scriptRe  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
)

// Strips LaTeXML HTML to heading-delimited Sections. Headings are marked with
// \x01 bytes before tag-stripping so they can still be split out afterward.
// The body before the first heading is dropped; the abstract backstop comes
// from the arXiv API.
// Input:
//   - raw: the raw HTML page body
//
// Output:
//   - []Section: one per heading found, with MethodRelevant set
func htmlToSections(raw string) []Section {
	raw = scriptRe.ReplaceAllString(raw, " ")
	raw = styleRe.ReplaceAllString(raw, " ")

	raw = headingRe.ReplaceAllString(raw, "\x01$1\x01")
	raw = anyTagRe.ReplaceAllString(raw, " ")
	raw = html.UnescapeString(raw)

	parts := strings.Split(raw, "\x01")
	var secs []Section
	for i := 1; i+1 < len(parts); i += 2 {
		heading := collapseWS(parts[i])
		body := collapseWS(parts[i+1])
		if heading == "" && body == "" {
			continue
		}
		secs = append(secs, Section{
			Heading:        heading,
			Body:           body,
			MethodRelevant: isMethodRelevant(heading),
		})
	}
	return secs
}

// Collapses any run of whitespace to a single space and trims the ends.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
