package pipeline

import (
	"strings"
	"testing"

	"codemypaper/internal/arxiv"
)

func TestValidateGenerate(t *testing.T) {
	outDir := t.TempDir()
	reserved := reservedNames(&arxiv.Paper{})

	validFiles := []File{
		{Name: "model.py"},
		{Name: smokeTestFile},
		{Name: "main.py"},
	}

	// Testing - a reply with all three required files, valid names
	// Input - model.py, smoke_test.py, main.py
	// Expected output - no error
	t.Run("all required files present", func(t *testing.T) {
		if err := validateGenerate(Reply{Files: validFiles}, outDir, reserved); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// Testing - a reply missing one required file
	// Input - model.py and smoke_test.py, no main.py
	// Expected output - error naming the missing file
	t.Run("missing required file", func(t *testing.T) {
		files := []File{{Name: "model.py"}, {Name: smokeTestFile}}
		err := validateGenerate(Reply{Files: files}, outDir, reserved)
		if err == nil || !strings.Contains(err.Error(), "main.py") {
			t.Fatalf("validateGenerate error = %v, want error naming main.py", err)
		}
	})

	// Testing - a file using a reserved name, case-varied
	// Input - required files plus "Imp_Details.MD"
	// Expected output - error, reserved-name check is case-insensitive
	t.Run("reserved name rejected", func(t *testing.T) {
		files := append(append([]File{}, validFiles...), File{Name: "Imp_Details.MD"})
		err := validateGenerate(Reply{Files: files}, outDir, reserved)
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("validateGenerate error = %v, want a reserved-name error", err)
		}
	})

	// Testing - a file path that escapes the output directory
	// Input - required files plus "../evil.py"
	// Expected output - error from the path jail
	t.Run("jail escape rejected", func(t *testing.T) {
		files := append(append([]File{}, validFiles...), File{Name: "../evil.py"})
		if err := validateGenerate(Reply{Files: files}, outDir, reserved); err == nil {
			t.Fatal("expected a jail error, got nil")
		}
	})
}

func TestValidateDebug(t *testing.T) {
	outDir := t.TempDir()
	reserved := reservedNames(&arxiv.Paper{})
	written := map[string]bool{"model.py": true, smokeTestFile: true, "main.py": true}

	// Testing - the debug reply only rewrites a file that was in the generate reply
	// Input - a reply touching "model.py" (already written)
	// Expected output - no error
	t.Run("rewrite of a written file", func(t *testing.T) {
		files := []File{{Name: "model.py"}}
		if err := validateDebug(Reply{Files: files}, outDir, reserved, written); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// Testing - the debug reply introduces a brand-new file not in the generate set
	// Input - a reply touching "requirements.txt" (never written)
	// Expected output - error, only existing files may be rewritten
	t.Run("new file rejected", func(t *testing.T) {
		files := []File{{Name: "requirements.txt"}}
		err := validateDebug(Reply{Files: files}, outDir, reserved, written)
		if err == nil || !strings.Contains(err.Error(), "rewrite existing files only") {
			t.Fatalf("validateDebug error = %v, want a subset-rule error", err)
		}
	})
}

func TestReservedNames(t *testing.T) {
	// Testing - the base reserved set with no persisted raw source
	// Input - &arxiv.Paper{} (RawName empty)
	// Expected output - run.log, paper.meta.json, imp_details.md, error_log.md present; no RawName entry
	t.Run("base set", func(t *testing.T) {
		r := reservedNames(&arxiv.Paper{})
		for _, want := range []string{"run.log", "paper.meta.json", "imp_details.md", "error_log.md"} {
			if !r[want] {
				t.Fatalf("reservedNames missing %q", want)
			}
		}
	})

	// Testing - a paper with a persisted raw source file
	// Input - &arxiv.Paper{RawName: "paper.html"}
	// Expected output - "paper.html" also present, lowercased
	t.Run("includes RawName", func(t *testing.T) {
		r := reservedNames(&arxiv.Paper{RawName: "paper.html"})
		if !r["paper.html"] {
			t.Fatal("reservedNames missing the paper's RawName")
		}
	})
}
