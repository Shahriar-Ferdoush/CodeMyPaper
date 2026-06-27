package arxiv

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// newStyleID matches a fully-anchored bare new-style arXiv id with an optional
// version suffix, e.g. "2401.01234" or "2401.01234v2". Capture group 1 is the
// bare id; group 2 (if present) is the dropped version.
var newStyleID = regexp.MustCompile(`^(\d{4}\.\d{4,5})(v\d+)?$`)

// ParseID normalizes any FR1 arXiv reference to its bare new-style id
// (NNNN.NNNNN, version suffix dropped).
//
// Accepted inputs include bare ids ("2401.01234"), versioned ids
// ("2401.01234v2"), and abs/pdf URLs over http or https, with or without a
// trailing ".pdf", a trailing slash, or a query string.
//
// Extraction is precise rather than a first-digit match: the input is trimmed,
// and if it parses as a URL with a host the last path segment is used (after
// stripping a trailing ".pdf" and slash); otherwise the trimmed string itself
// is used. A fully anchored regex then validates that segment.
//
// Old-style ids ("cs/0501001"), empty input, and anything else return a
// descriptive error.
func ParseID(idOrURL string) (string, error) {
	s := strings.TrimSpace(idOrURL)
	if s == "" {
		return "", fmt.Errorf("arxiv: empty id or URL")
	}

	segment := s
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		p := u.Path
		p = strings.TrimSuffix(p, "/")
		p = strings.TrimSuffix(p, ".pdf")
		p = strings.TrimSuffix(p, "/")
		// Last path segment.
		if i := strings.LastIndex(p, "/"); i >= 0 {
			segment = p[i+1:]
		} else {
			segment = p
		}
	}

	m := newStyleID.FindStringSubmatch(segment)
	if m == nil {
		return "", fmt.Errorf("arxiv: %q is not a supported new-style arXiv id (expected NNNN.NNNNN)", idOrURL)
	}
	return m[1], nil
}
