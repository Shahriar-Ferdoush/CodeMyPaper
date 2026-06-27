package arxiv

import "testing"

func TestParseID(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "bare id", in: "2401.01234", want: "2401.01234"},
		{name: "version stripped", in: "2401.01234v2", want: "2401.01234"},
		{name: "abs url", in: "https://arxiv.org/abs/2401.01234", want: "2401.01234"},
		{name: "pdf url", in: "https://arxiv.org/pdf/2401.01234", want: "2401.01234"},
		{name: "pdf extension", in: "https://arxiv.org/pdf/2401.01234.pdf", want: "2401.01234"},
		{name: "http scheme", in: "http://arxiv.org/abs/2401.01234", want: "2401.01234"},
		{name: "trailing slash", in: "https://arxiv.org/abs/2401.01234/", want: "2401.01234"},
		{name: "query string", in: "https://arxiv.org/abs/2401.01234?context=cs", want: "2401.01234"},
		{name: "five digit suffix", in: "2401.12345", want: "2401.12345"},
		{name: "abs url versioned", in: "https://arxiv.org/abs/2401.01234v3", want: "2401.01234"},
		{name: "old style id", in: "cs/0501001", wantErr: true},
		{name: "old style abs url", in: "https://arxiv.org/abs/cs/0501001", wantErr: true},
		{name: "garbage", in: "not-an-arxiv-id", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseID(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseID(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
