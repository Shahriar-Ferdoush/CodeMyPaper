// Package arxiv turns an arXiv id or URL into method-focused paper text:
// id parsing, the three-rung source ladder, section extraction, and the
// rerun cache.
package arxiv

import "fmt"

// CachedMeta is the shape of the paper.meta.json sidecar written alongside a fetched
// paper's raw source, letting a later run rebuild the same Paper with no network call.
type CachedMeta struct {
	Source   string `json:"source"`
	RawName  string `json:"raw_name"`
	Title    string `json:"title"`
	Abstract string `json:"abstract"`
}

// FromCache rebuilds a Paper from a previously-persisted raw source and its sidecar
// metadata, re-running the same pure parse step Fetch would have used for
// that rung. No network calls.
// Input:
//   - id: the canonical arXiv id
//   - meta: the sidecar metadata (source, raw filename, title, abstract)
//   - raw: the raw bytes previously persisted at meta.RawName
//
// Output:
//   - *Paper: the rebuilt paper
//   - error: on an unrecognized RawName, or the rung's parser yielding zero sections
func FromCache(id string, meta CachedMeta, raw []byte) (*Paper, error) {
	var secs []Section
	switch meta.RawName {
	case "paper.html":
		secs = htmlToSections(string(raw))
	case "paper.tar.gz", "paper.tex.gz":
		tex, _, err := extractEprintTeX(raw)
		if err != nil {
			return nil, fmt.Errorf("cache: %w", err)
		}
		secs = latexSections(tex)
	default:
		return nil, fmt.Errorf("cache: unrecognized raw_name %q", meta.RawName)
	}

	if len(secs) == 0 {
		return nil, fmt.Errorf("cache: %s parsed to zero sections", meta.RawName)
	}

	return &Paper{
		ID:       id,
		Title:    meta.Title,
		Abstract: meta.Abstract,
		Sections: secs,
		Source:   meta.Source,
		Raw:      raw,
		RawName:  meta.RawName,
	}, nil
}
