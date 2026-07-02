package paths

import "testing"

func TestValidateDownloadLocation(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		allowAbsolute bool
		wantErr       bool
	}{
		{"relative clean", "downloads", false, false},
		{"relative nested", "a/b/c", false, false},
		{"absolute allowed cli", "/tmp/vids", true, false},
		{"absolute rejected api", "/tmp/vids", false, true},
		{"traversal rejected", "../etc", true, true},
		{"mid traversal rejected", "foo/../bar/../../etc", true, true},
		{"empty rejected", "   ", false, true},
		{"whitespace trimmed", "  downloads  ", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateDownloadLocation(tc.path, tc.allowAbsolute)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.path, err)
			}
			if got == "" {
				t.Errorf("expected non-empty cleaned path for %q", tc.path)
			}
		})
	}
}

func TestValidateDownloadLocation_TraversalNeverAccepted(t *testing.T) {
	// Traversal must be rejected even when absolute paths are allowed.
	for _, p := range []string{"../x", "a/../../b", "../etc/passwd"} {
		if _, err := ValidateDownloadLocation(p, true); err == nil {
			t.Errorf("expected traversal rejection for %q", p)
		}
	}
}
