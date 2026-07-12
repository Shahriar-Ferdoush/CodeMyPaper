package arxiv

import (
	"fmt"
	"regexp"
	"strings"
)

// arxivPrefix, modernID, legacyID match the two canonical arXiv id forms (modern
// YYMM.NNNNN and legacy archive/YYMMNNN) and the "arXiv:" prefix, with any trailing
// version already excluded from the match.
var (
	arxivPrefix = regexp.MustCompile(`(?i)arxiv:`)
	modernID    = regexp.MustCompile(`\d{4}\.\d{4,5}`)
	legacyID    = regexp.MustCompile(`[a-z][a-z-]*(?:\.[A-Z]{2})?/\d{7}`)
)

// ParseID normalizes any arXiv id/URL form into a bare canonical id (version stripped).
// Tries the unambiguous modern form before the legacy form, so a URL's host/path is
// never misread as a legacy id.
// Input:
//   - s: a bare id or an /abs//pdf//html//e-print/ URL, with or without an "arXiv:" prefix
//
// Output:
//   - string: the canonical id
//   - error: if s contains no recognizable arXiv id
func ParseID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty arXiv id")
	}
	s = strings.TrimSuffix(s, ".pdf")
	s = arxivPrefix.ReplaceAllString(s, "")

	if m := modernID.FindString(s); m != "" {
		return m, nil
	}
	if m := legacyID.FindString(s); m != "" {
		return m, nil
	}
	return "", fmt.Errorf("not a recognizable arXiv id or URL: %q", s)
}
