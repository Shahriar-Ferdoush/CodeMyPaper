package arxiv

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// The last rung of the fetch ladder: download the e-print tarball, gunzip it,
// and lightly strip the concatenated LaTeX source into Sections.
// Input:
//   - ctx: context.Context for cancellation and deadlines
//   - id: the canonical arXiv id
//
// Output:
//   - []Section: the parsed sections
//   - []byte: the raw gzipped payload as served, for persisting
//   - string: a filename for the payload ("paper.tar.gz" or "paper.tex.gz")
//   - error: on a failed download or gunzip
func fetchEprintSections(ctx context.Context, id string) ([]Section, []byte, string, error) {
	body, err := httpGet(ctx, eprintBase+id)
	if err != nil {
		return nil, nil, "", err
	}
	tex, isTarFile, err := extractEprintTeX(body)
	if err != nil {
		return nil, nil, "", err
	}
	name := "paper.tex.gz"
	if isTarFile {
		name = "paper.tar.gz"
	}
	return latexSections(tex), body, name, nil
}

// Gunzips the e-print payload and returns the concatenated .tex source.
// Input:
//   - gz: the gzipped e-print response body
//
// Output:
//   - string: the concatenated .tex source
//   - bool: whether the payload was a tarball (vs a bare gzipped .tex)
//   - error: on a gunzip/decompress failure or an untarrable payload
func extractEprintTeX(gz []byte) (string, bool, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return "", false, fmt.Errorf("e-print: gunzip: %w", err)
	}
	defer zr.Close()

	dec, err := io.ReadAll(zr)
	if err != nil {
		return "", false, fmt.Errorf("e-print: decompress: %w", err)
	}
	if isTar(dec) {
		tex, err := concatTarTeX(dec)
		return tex, true, err
	}
	// arXiv quirk: single-file submissions are a bare gzipped .tex, not a tar.
	// Decompressed bytes are already the source.
	return string(dec), false, nil
}

// Reports whether b begins with a POSIX tar header (ustar magic at offset 257).
func isTar(b []byte) bool {
	return len(b) >= 263 && string(b[257:262]) == "ustar"
}

// Untars b and concatenates the content of every .tex member.
// Input:
//   - b: a POSIX tar byte stream
//
// Output:
//   - string: the concatenated .tex source
//   - error: on a malformed tar or no .tex members found
func concatTarTeX(b []byte) (string, error) {
	tr := tar.NewReader(bytes.NewReader(b))
	var sb strings.Builder
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("e-print: untar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(strings.ToLower(hdr.Name), ".tex") {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return "", fmt.Errorf("e-print: read %s: %w", hdr.Name, err)
		}
		sb.Write(data)
		sb.WriteString("\n")
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("e-print: tarball contained no .tex files")
	}
	return sb.String(), nil
}

// latexComment, latexSection, latexEnvCmd, latexCmd match LaTeX comments, \section{...}
// headings, \begin/\end environments, and other commands respectively.
var (
	latexComment = regexp.MustCompile(`(?m)%.*$`)
	latexSection = regexp.MustCompile(`\\section\*?\{([^}]*)\}`)
	latexEnvCmd  = regexp.MustCompile(`\\(?:begin|end)\{[^}]*\}`)
	latexCmd     = regexp.MustCompile(`\\[a-zA-Z]+\*?(?:\[[^\]]*\])?`)
)

// Splits LaTeX source into Sections on \section{...} markers. With no markers
// it returns the whole cleaned body as a single MethodRelevant section.
// Input:
//   - tex: the LaTeX source
//
// Output:
//   - []Section: one per \section marker, or one covering the whole body if none found
func latexSections(tex string) []Section {
	tex = latexComment.ReplaceAllString(tex, "")

	locs := latexSection.FindAllStringSubmatchIndex(tex, -1)
	if len(locs) == 0 {
		body := cleanLatex(tex)
		if body == "" {
			return nil
		}
		return []Section{{Body: body, MethodRelevant: true}}
	}

	var secs []Section
	for i, loc := range locs {
		heading := collapseWS(tex[loc[2]:loc[3]])
		start := loc[1]
		end := len(tex)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := cleanLatex(tex[start:end])
		secs = append(secs, Section{
			Heading:        heading,
			Body:           body,
			MethodRelevant: isMethodRelevant(heading),
		})
	}
	return secs
}

// Performs a light strip: drop \begin/\end env markers and command tokens,
// drop braces and math delimiters, and keep the argument text. Degraded but readable.
func cleanLatex(s string) string {
	s = latexEnvCmd.ReplaceAllString(s, " ")
	s = latexCmd.ReplaceAllString(s, " ")
	s = strings.NewReplacer("{", " ", "}", " ", "$", " ", "&", " ").Replace(s)
	return collapseWS(s)
}
