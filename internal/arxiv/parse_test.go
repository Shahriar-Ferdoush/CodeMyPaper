package arxiv

import "testing"

func TestParseID(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			// Testing - a bare modern id
			// Input - "2401.01234"
			// Expected output - unchanged
			name:  "bare modern id",
			input: "2401.01234",
			want:  "2401.01234",
		},
		{
			// Testing - a bare legacy id
			// Input - "hep-th/9901001"
			// Expected output - unchanged
			name:  "bare legacy id",
			input: "hep-th/9901001",
			want:  "hep-th/9901001",
		},
		{
			// Testing - a case-insensitive "arXiv:" prefix
			// Input - "arXiv:2401.01234"
			// Expected output - prefix stripped
			name:  "arXiv prefix",
			input: "arXiv:2401.01234",
			want:  "2401.01234",
		},
		{
			// Testing - a full /abs/ URL
			// Input - "https://arxiv.org/abs/2401.01234"
			// Expected output - id extracted from the URL
			name:  "abs URL",
			input: "https://arxiv.org/abs/2401.01234",
			want:  "2401.01234",
		},
		{
			// Testing - a full /pdf/ URL with a .pdf suffix
			// Input - "https://arxiv.org/pdf/2401.01234.pdf"
			// Expected output - id extracted, no ".pdf" residue
			name:  "pdf URL",
			input: "https://arxiv.org/pdf/2401.01234.pdf",
			want:  "2401.01234",
		},
		{
			// Testing - a versioned id
			// Input - "2401.01234v2"
			// Expected output - version suffix stripped
			name:  "versioned id",
			input: "2401.01234v2",
			want:  "2401.01234",
		},
		{
			// Testing - a legacy id with a subclass
			// Input - "math.AG/0601001"
			// Expected output - unchanged
			name:  "legacy id with subclass",
			input: "math.AG/0601001",
			want:  "math.AG/0601001",
		},
		{
			// Testing - garbage input with no recognizable id
			// Input - "not an id"
			// Expected output - error
			name:    "garbage input",
			input:   "not an id",
			wantErr: true,
		},
		{
			// Testing - an empty string
			// Input - ""
			// Expected output - error
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseID(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseID(%q) = %q, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseID(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
