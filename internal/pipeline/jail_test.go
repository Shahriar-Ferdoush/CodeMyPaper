package pipeline

import (
	"strconv"
	"strings"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	base := t.TempDir()

	cases := []struct {
		name    string
		rel     string
		wantErr bool
	}{
		{
			// Testing - an in-dir relative path
			// Input - "model.py"
			// Expected output - allowed, no error
			name: "in-dir file",
			rel:  "model.py",
		},
		{
			// Testing - a nested in-dir relative path
			// Input - "pkg/model.py"
			// Expected output - allowed, no error
			name: "nested in-dir file",
			rel:  "pkg/model.py",
		},
		{
			// Testing - bare parent-dir escape
			// Input - ".."
			// Expected output - rejected
			name:    "bare dotdot",
			rel:     "..",
			wantErr: true,
		},
		{
			// Testing - a path that climbs out then goes elsewhere
			// Input - "../etc/passwd"
			// Expected output - rejected
			name:    "climb then escape",
			rel:     "../etc/passwd",
			wantErr: true,
		},
		{
			// Testing - a path that climbs out twice, netting outside base
			// Input - "out/../../etc"
			// Expected output - rejected
			name:    "net escape",
			rel:     "out/../../etc",
			wantErr: true,
		},
		{
			// Testing - an absolute path
			// Input - "/etc/passwd"
			// Expected output - rejected
			name:    "absolute path",
			rel:     "/etc/passwd",
			wantErr: true,
		},
		{
			// Testing - an empty path
			// Input - ""
			// Expected output - rejected
			name:    "empty path",
			rel:     "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeJoin(base, tc.rel)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("safeJoin(%q, %q) = %q, want error", base, tc.rel, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeJoin(%q, %q) unexpected error: %v", base, tc.rel, err)
			}
			if !strings.HasPrefix(got, base) {
				t.Fatalf("safeJoin(%q, %q) = %q, want prefix %q", base, tc.rel, got, base)
			}
		})
	}
}

func TestCapOutput(t *testing.T) {
	// Testing - input shorter than the cap
	// Input - "hello", max 100
	// Expected output - returned unchanged
	t.Run("under cap", func(t *testing.T) {
		s := "hello"
		got := capOutput(s, 100)
		if got != s {
			t.Fatalf("capOutput(%q, 100) = %q, want unchanged", s, got)
		}
	})

	// Testing - input longer than the cap
	// Input - a 100-byte string, max 10
	// Expected output - truncated to max bytes plus a marker naming the omitted byte count
	t.Run("over cap", func(t *testing.T) {
		s := strings.Repeat("a", 100)
		limit := 10
		got := capOutput(s, limit)

		if !strings.HasPrefix(got, s[:limit]) {
			t.Fatalf("capOutput result does not start with the first %d bytes", limit)
		}
		wantOmitted := len(s) - limit
		if !strings.Contains(got, "... [output truncated: "+strconv.Itoa(wantOmitted)+" bytes omitted]") {
			t.Fatalf("capOutput(%q, %d) = %q, missing truncation marker for %d bytes", s, limit, got, wantOmitted)
		}
	})
}
