package arxiv

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// userAgent identifies the tool to arXiv.
const userAgent = "codemypaper/0.1 (arXiv reference-implementation tool)"

// maxBodyBytes caps any single response read into memory.
const maxBodyBytes = 16 << 20 // 16 MiB

// ErrSourcesExhausted is returned when every source in the fetch ladder fails and the
// API metadata is also empty.
var ErrSourcesExhausted = errors.New("arxiv: all sources failed (arxiv.org/html, ar5iv, e-print)")

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Source endpoints, package-level so tests can repoint them at an httptest server.
// All HTTPS — title/abstract flow into the model prompt, so a plaintext hop is not acceptable.
var (
	arxivHTMLBase = "https://arxiv.org/html/"
	ar5ivHTMLBase = "https://ar5iv.labs.arxiv.org/html/"
	eprintBase    = "https://arxiv.org/e-print/"
	arxivAPIBase  = "https://export.arxiv.org/api/query?id_list="
)

// Fetch resolves idOrURL and builds a Paper from the first source in the ladder that
// yields sections: arxiv.org/html → ar5iv mirror → e-print tarball. Title and abstract
// always come from the arXiv API as a backstop.
// Input:
//   - ctx: context.Context for cancellation and deadlines
//   - idOrURL: a bare arXiv id or any /abs//pdf//html//e-print/ URL form
//
// Output:
//   - *Paper: the ingested paper
//   - error: ParseID's error, or ErrSourcesExhausted if no sections and no abstract
func Fetch(ctx context.Context, idOrURL string) (*Paper, error) {
	id, err := ParseID(idOrURL)
	if err != nil {
		return nil, err
	}

	p := &Paper{ID: id}

	// Fetch title + abstract from the arXiv API, even if we later get sections from a different source.
	if title, abstract, err := fetchMeta(ctx, id); err == nil {
		p.Title, p.Abstract = title, abstract
	}

	// Try sources in order:
	// 	- arxiv.org HTML (LaTeXML) → ar5iv mirror → e-print tarball
	if secs, raw, err := fetchHTMLSections(ctx, arxivHTMLBase+id); err == nil && len(secs) > 0 {
		p.Sections, p.Source = secs, "arxiv-html"
		p.Raw, p.RawName = raw, "paper.html"
	} else if secs, raw, err := fetchHTMLSections(ctx, ar5ivHTMLBase+id); err == nil && len(secs) > 0 {
		p.Sections, p.Source = secs, "ar5iv"
		p.Raw, p.RawName = raw, "paper.html"
	} else if secs, raw, name, err := fetchEprintSections(ctx, id); err == nil && len(secs) > 0 {
		p.Sections, p.Source = secs, "eprint"
		p.Raw, p.RawName = raw, name
	}

	if len(p.Sections) == 0 && p.Abstract == "" {
		return nil, ErrSourcesExhausted
	}
	if len(p.Sections) == 0 {
		p.Source = "api-only"
	}
	return p, nil
}

// fetchHTMLSections GETs a LaTeXML HTML page and extracts its sections, also returning
// the raw page bytes so the caller can persist the exact fetched source.
func fetchHTMLSections(ctx context.Context, pageURL string) ([]Section, []byte, error) {
	body, err := httpGet(ctx, pageURL)
	if err != nil {
		return nil, nil, err
	}
	return htmlToSections(string(body)), body, nil
}

// fetchMeta pulls title + abstract from the arXiv Atom API. Both fields carry the LaTeX
// source's line-wrapping (stray newlines/spaces), so they're whitespace-collapsed.
func fetchMeta(ctx context.Context, id string) (title, abstract string, err error) {
	api := arxivAPIBase + url.QueryEscape(id)
	body, err := httpGet(ctx, api)
	if err != nil {
		return "", "", err
	}
	var feed struct {
		Entries []struct {
			Title   string `xml:"title"`
			Summary string `xml:"summary"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return "", "", fmt.Errorf("arxiv api: decode: %w", err)
	}
	if len(feed.Entries) == 0 {
		return "", "", fmt.Errorf("arxiv api: no entry for %s", id)
	}
	return collapseWS(feed.Entries[0].Title), collapseWS(feed.Entries[0].Summary), nil
}

// httpGet performs a context-aware GET with the tool's User-Agent and reads a capped body.
func httpGet(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get %s: status %s", target, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
}
