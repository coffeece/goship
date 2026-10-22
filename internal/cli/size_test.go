package cli

import "testing"

func TestParseSizeMB(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int // 0 = nil
		bad  bool
	}{
		{"none", 0, false}, {"NONE", 0, false},
		{"500", 500, false}, {"500MB", 500, false}, {"500mb", 500, false},
		{"2GB", 2048, false}, {"2gb", 2048, false}, {"1.5GB", 1536, false}, {" 10 ", 10, false},
		{"9", 0, true}, {"0", 0, true}, {"-5", 0, true}, {"", 0, true}, {"abc", 0, true}, {"GB", 0, true}, {"0.001GB", 0, true},
	} {
		got, err := parseSizeMB(tc.in)
		switch {
		case tc.bad:
			if err == nil {
				t.Errorf("parseSizeMB(%q) = %v, want error", tc.in, got)
			}
		case err != nil:
			t.Errorf("parseSizeMB(%q): %v", tc.in, err)
		case tc.want == 0 && got != nil, tc.want != 0 && (got == nil || *got != tc.want):
			t.Errorf("parseSizeMB(%q) = %v, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseDiskGB(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		bad  bool
	}{
		{"none", 0, false}, {"None", 0, false}, {"100", 100, false}, {"100GB", 100, false}, {"100gb", 100, false},
		{"0", 0, false}, {"-1", 0, true}, {"1.5", 0, true}, {"", 0, true}, {"big", 0, true},
	} {
		got, err := parseDiskGB(tc.in)
		if tc.bad != (err != nil) || got != tc.want {
			t.Errorf("parseDiskGB(%q) = %d, %v", tc.in, got, err)
		}
	}
}
