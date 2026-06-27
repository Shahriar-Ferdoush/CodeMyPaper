package arxiv

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const userAgent = "codemypaper/dev"

// httpClient is the single shared client for all outbound arXiv/ar5iv requests.
// It follows redirects by default (ar5iv redirects bare ids to versioned URLs).
var httpClient = &http.Client{}

// minBodyBytes is the validity threshold below which ar5iv output (with no
// usable sections) is treated as a failed conversion. ar5iv often serves a 200
// "we are processing this article" stub; ~200 bytes of real text is the floor
// for usable content.
const minBodyBytes = 200

// get issues a ctx-bound GET with the shared client and the codemypaper
// User-Agent. The caller owns closing the body.
func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return httpClient.Do(req)
}

// Fetch resolves idOrURL to a bare id, then builds a Paper from arXiv sources.
//
// Title/abstract come from the arXiv API (non-fatal: left empty on failure).
// Body text comes from ar5iv HTML, falling back to the e-print LaTeX tarball
// when ar5iv returns a non-200/transport error or yields no usable content.
// Fetch errors only if both body sources fail. maxChars is stored on the Paper
// for PromptText's budget.
func Fetch(ctx context.Context, idOrURL string, maxChars int) (*Paper, error) {
	id, err := ParseID(idOrURL)
	if err != nil {
		return nil, err
	}

	p := &Paper{ID: id, maxChars: maxChars}

	// Title/abstract — non-fatal.
	if title, abstract, err := fetchMeta(ctx, id); err == nil {
		p.Title = title
		p.Abstract = abstract
	}

	// Body: ar5iv first, then e-print tarball. The e-print fallback fires when
	// ar5iv errors (non-200/transport) or returns no usable content; the
	// arXiv-API abstract has no bearing on whether ar5iv succeeded.
	sections, ar5ivErr := fetchAr5iv(ctx, id)
	if ar5ivErr == nil && ar5ivContentValid(sections) {
		p.Sections = sections
		p.Source = "ar5iv"
		return p, nil
	}

	eprintSections, eprintErr := fetchEprint(ctx, id)
	if eprintErr == nil {
		p.Sections = eprintSections
		p.Source = "eprint"
		return p, nil
	}

	return nil, fmt.Errorf("arxiv: failed to fetch body for %s (ar5iv: %v; e-print: %v)", id, ar5ivErr, eprintErr)
}

// ar5ivContentValid reports whether ar5iv's own output is usable. It judges
// only the sections parsed from ar5iv HTML — the arXiv-API abstract is
// intentionally not a factor, so an ar5iv "processing this article" stub (zero
// sections) is correctly rejected and the e-print fallback fires.
func ar5ivContentValid(sections []Section) bool {
	if len(sections) == 0 {
		return false
	}
	total := 0
	for _, s := range sections {
		total += len(s.Body)
	}
	return total >= minBodyBytes
}

// --- arXiv API (Atom XML) ---

type atomFeed struct {
	Entries []struct {
		Title   string `xml:"title"`
		Summary string `xml:"summary"`
	} `xml:"entry"`
}

func fetchMeta(ctx context.Context, id string) (title, abstract string, err error) {
	resp, err := get(ctx, "http://export.arxiv.org/api/query?id_list="+id)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("arxiv api: status %s", resp.Status)
	}
	var feed atomFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return "", "", err
	}
	if len(feed.Entries) == 0 {
		return "", "", fmt.Errorf("arxiv api: no entry for %s", id)
	}
	return collapseWhitespace(feed.Entries[0].Title), collapseWhitespace(feed.Entries[0].Summary), nil
}

// --- ar5iv HTML ---

var (
	wsRe         = regexp.MustCompile(`[ \t\f\r]+`)
	blankLinesRe = regexp.MustCompile(`\n{3,}`)
)

// maxAr5ivBytes caps the ar5iv HTML response read so a pathological/huge
// document can't exhaust memory before parsing.
const maxAr5ivBytes = 32 << 20

func fetchAr5iv(ctx context.Context, id string) ([]Section, error) {
	resp, err := get(ctx, "https://ar5iv.org/html/"+id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ar5iv: status %s", resp.Status)
	}
	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxAr5ivBytes))
	if err != nil {
		return nil, fmt.Errorf("ar5iv: parse: %w", err)
	}
	return parseAr5ivHTML(doc), nil
}

// parseAr5ivHTML walks an ar5iv document with goquery and returns one Section
// per heading-bearing block.
//
// It first drops script/style/noscript nodes (their text is never content).
// ar5iv wraps each section in a <section> element whose first heading
// (h1–h4, including the <h6 class="ltx_title"> used for the abstract) names it
// and whose .Text() is the section body; that is the primary path. When no
// <section> elements exist it falls back to slicing the document at h2/h3
// headings, and as a last resort returns the whole <body> as one section.
//
// goquery's .Text() already decodes HTML entities, so no html.UnescapeString is
// needed; whitespace is collapsed via collapseWhitespace.
func parseAr5ivHTML(doc *goquery.Document) []Section {
	doc.Find("script, style, noscript").Remove()

	if secs := sectionsFromElements(doc); len(secs) > 0 {
		return secs
	}
	if secs := sectionsFromHeadings(doc); len(secs) > 0 {
		return secs
	}
	body := collapseWhitespace(doc.Find("body").Text())
	if body == "" {
		return nil
	}
	return []Section{{Heading: "Body", Body: body, MethodRelevant: true}}
}

// sectionsFromElements builds one Section per ar5iv <section> element, taking
// the first heading inside (h1–h4, plus ltx_title for the abstract) as the
// heading and the element's collapsed text as the body.
func sectionsFromElements(doc *goquery.Document) []Section {
	var sections []Section
	doc.Find("section").Each(func(_ int, s *goquery.Selection) {
		heading := collapseWhitespace(s.Find("h1,h2,h3,h4,.ltx_title").First().Text())
		body := collapseWhitespace(s.Text())
		if heading == "" || body == "" {
			return
		}
		sections = append(sections, Section{
			Heading:        heading,
			Body:           body,
			MethodRelevant: isMethodRelevant(heading),
		})
	})
	return sections
}

// sectionsFromHeadings is the fallback for ar5iv markup without <section>
// wrappers: it splits the flat document at h2/h3 headings, using each heading's
// text and the text of all following siblings up to the next heading.
func sectionsFromHeadings(doc *goquery.Document) []Section {
	var sections []Section
	doc.Find("h2, h3").Each(func(_ int, h *goquery.Selection) {
		heading := collapseWhitespace(h.Text())
		if heading == "" {
			return
		}
		var body strings.Builder
		for sib := h.Next(); sib.Length() > 0; sib = sib.Next() {
			if sib.Is("h2, h3") {
				break
			}
			body.WriteString(sib.Text())
			body.WriteString("\n")
		}
		sections = append(sections, Section{
			Heading:        heading,
			Body:           collapseWhitespace(body.String()),
			MethodRelevant: isMethodRelevant(heading),
		})
	})
	return sections
}

// collapseWhitespace collapses horizontal whitespace runs and trims blank-line
// runs, leaving readable single-blank-line-separated paragraphs.
func collapseWhitespace(s string) string {
	s = wsRe.ReplaceAllString(s, " ")
	// Trim trailing spaces on each line.
	s = strings.ReplaceAll(s, " \n", "\n")
	s = strings.ReplaceAll(s, "\n ", "\n")
	s = blankLinesRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// --- e-print tarball (gzip + tar of .tex) ---

// maxEprintBytes caps the total .tex bytes accumulated from the e-print
// tarball, so a single huge entry (or many entries) can't exhaust memory.
const maxEprintBytes = 32 << 20

var texSectionRe = regexp.MustCompile(`(?s)\\section\*?\{(.*?)\}`)

func fetchEprint(ctx context.Context, id string) ([]Section, error) {
	resp, err := get(ctx, "https://arxiv.org/e-print/"+id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("e-print: status %s", resp.Status)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("e-print: gzip: %w", err)
	}
	defer gz.Close()

	var tex strings.Builder
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("e-print: tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(strings.ToLower(hdr.Name), ".tex") {
			continue
		}
		// Cap the total accumulated .tex so a huge entry can't exhaust memory.
		remaining := maxEprintBytes - int64(tex.Len())
		if remaining <= 0 {
			break
		}
		if _, err := io.Copy(&tex, io.LimitReader(tr, remaining)); err != nil {
			return nil, fmt.Errorf("e-print: read %s: %w", hdr.Name, err)
		}
		tex.WriteString("\n")
	}

	body := tex.String()
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("e-print: no .tex content for %s", id)
	}
	return splitTexSections(body), nil
}

// splitTexSections does a best-effort split of concatenated LaTeX on
// \section{...} markers. If none are found, the whole body is returned as a
// single method-relevant section. Headings are matched against the method
// keywords; the preamble before the first \section is dropped.
func splitTexSections(tex string) []Section {
	locs := texSectionRe.FindAllStringSubmatchIndex(tex, -1)
	if len(locs) == 0 {
		return []Section{{
			Heading:        "Source (LaTeX)",
			Body:           strings.TrimSpace(tex),
			MethodRelevant: true,
		}}
	}

	var sections []Section
	for i, loc := range locs {
		heading := collapseWhitespace(tex[loc[2]:loc[3]])
		bodyStart := loc[1]
		bodyEnd := len(tex)
		if i+1 < len(locs) {
			bodyEnd = locs[i+1][0]
		}
		sections = append(sections, Section{
			Heading:        heading,
			Body:           strings.TrimSpace(tex[bodyStart:bodyEnd]),
			MethodRelevant: isMethodRelevant(heading),
		})
	}
	return sections
}
