package downloader

import (
	"bufio"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
)

func TestParsePlaylist(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		id                 int
		title              string
		seqNo              int
		wantKeyURL         string
		wantFirstViewURLs  []string
		wantSecondViewURLs []string
		wantHasMultiple    bool
	}{
		{
			name: "single view playlist",
			input: `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://placeholder.test/key"
#EXTINF:1
chunk1.ts
#EXTINF:1
chunk2.ts
#EXT-X-ENDLIST`,
			id:                 123,
			title:              "Lecture 1",
			seqNo:              1,
			wantKeyURL:         "https://placeholder.test/key",
			wantFirstViewURLs:  []string{"chunk1.ts", "chunk2.ts"},
			wantSecondViewURLs: nil,
			wantHasMultiple:    false,
		},
		{
			name: "dual view playlist with discontinuity",
			input: `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://placeholder.test/key"
#EXTINF:1
first_chunk1.ts
#EXTINF:1
first_chunk2.ts
#EXT-X-DISCONTINUITY
#EXTINF:1
second_chunk1.ts
#EXTINF:1
second_chunk2.ts
#EXTINF:1
second_chunk3.ts
#EXT-X-ENDLIST`,
			id:                 456,
			title:              "Lecture 2",
			seqNo:              2,
			wantKeyURL:         "https://placeholder.test/key",
			wantFirstViewURLs:  []string{"first_chunk1.ts", "first_chunk2.ts"},
			wantSecondViewURLs: []string{"second_chunk1.ts", "second_chunk2.ts", "second_chunk3.ts"},
			wantHasMultiple:    true,
		},
		{
			name: "no key URL",
			input: `#EXTM3U
#EXTINF:1
chunk1.ts
#EXTINF:1
chunk2.ts
#EXT-X-ENDLIST`,
			id:                 789,
			title:              "No Key Lecture",
			seqNo:              3,
			wantKeyURL:         "",
			wantFirstViewURLs:  []string{"chunk1.ts", "chunk2.ts"},
			wantSecondViewURLs: nil,
			wantHasMultiple:    false,
		},
		{
			name: "empty playlist",
			input: `#EXTM3U
#EXT-X-ENDLIST`,
			id:                 100,
			title:              "Empty Lecture",
			seqNo:              4,
			wantKeyURL:         "",
			wantFirstViewURLs:  nil,
			wantSecondViewURLs: nil,
			wantHasMultiple:    false,
		},
		{
			name: "key URL with special characters",
			input: `#EXTM3U
#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://placeholder.test/path?param=value"
#EXTINF:1
chunk1.ts
#EXT-X-ENDLIST`,
			id:                 200,
			title:              "Special Key Lecture",
			seqNo:              5,
			wantKeyURL:         "https://placeholder.test/path?param=value",
			wantFirstViewURLs:  []string{"chunk1.ts"},
			wantSecondViewURLs: nil,
			wantHasMultiple:    false,
		},
		{
			name: "multiple discontinuities all go to second view",
			input: `#EXTM3U
#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://placeholder.test/key"
#EXTINF:1
view1_chunk1.ts
#EXT-X-DISCONTINUITY
view2_chunk1.ts
#EXT-X-DISCONTINUITY
view3_chunk1.ts
#EXT-X-ENDLIST`,
			id:                 300,
			title:              "Multi-Discontinuity",
			seqNo:              6,
			wantKeyURL:         "https://placeholder.test/key",
			wantFirstViewURLs:  []string{"view1_chunk1.ts"},
			wantSecondViewURLs: []string{"view2_chunk1.ts", "view3_chunk1.ts"},
			wantHasMultiple:    true,
		},
		{
			name: "playlist with EXT-X-TARGETDURATION",
			input: `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:11
#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://placeholder.test/key"
#EXTINF:10.0
chunk1.ts
#EXTINF:10.0
chunk2.ts
#EXT-X-ENDLIST`,
			id:                 400,
			title:              "With Target Duration",
			seqNo:              7,
			wantKeyURL:         "https://placeholder.test/key",
			wantFirstViewURLs:  []string{"chunk1.ts", "chunk2.ts"},
			wantSecondViewURLs: nil,
			wantHasMultiple:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(tt.input))
			result, err := client.ParsePlaylist(scanner, tt.id, tt.title, tt.seqNo)
			if err != nil {
				t.Fatalf("ParsePlaylist() unexpected error: %v", err)
			}

			if result.ID != tt.id {
				t.Errorf("ID = %d, want %d", result.ID, tt.id)
			}

			if result.Title != tt.title {
				t.Errorf("Title = %q, want %q", result.Title, tt.title)
			}

			if result.SeqNo != tt.seqNo {
				t.Errorf("SeqNo = %d, want %d", result.SeqNo, tt.seqNo)
			}

			if result.KeyURL != tt.wantKeyURL {
				t.Errorf("KeyURL = %q, want %q", result.KeyURL, tt.wantKeyURL)
			}

			if len(result.FirstViewURLs) != len(tt.wantFirstViewURLs) {
				t.Errorf("FirstViewURLs len = %d, want %d", len(result.FirstViewURLs), len(tt.wantFirstViewURLs))
			} else {
				for i := range result.FirstViewURLs {
					if result.FirstViewURLs[i] != tt.wantFirstViewURLs[i] {
						t.Errorf("FirstViewURLs[%d] = %q, want %q", i, result.FirstViewURLs[i], tt.wantFirstViewURLs[i])
					}
				}
			}

			if len(result.SecondViewURLs) != len(tt.wantSecondViewURLs) {
				t.Errorf("SecondViewURLs len = %d, want %d", len(result.SecondViewURLs), len(tt.wantSecondViewURLs))
			} else {
				for i := range result.SecondViewURLs {
					if result.SecondViewURLs[i] != tt.wantSecondViewURLs[i] {
						t.Errorf("SecondViewURLs[%d] = %q, want %q", i, result.SecondViewURLs[i], tt.wantSecondViewURLs[i])
					}
				}
			}

			if result.HasMultipleViews != tt.wantHasMultiple {
				t.Errorf("HasMultipleViews = %v, want %v", result.HasMultipleViews, tt.wantHasMultiple)
			}
		})
	}
}

func TestParsePlaylistKeyURLPattern(t *testing.T) {
	tests := []struct {
		name       string
		keyLine    string
		wantKeyURL string
	}{
		{
			name:       "simple URI",
			keyLine:    `#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://placeholder.test/key"`,
			wantKeyURL: "https://placeholder.test/key",
		},
		{
			name:       "URI with equals in value",
			keyLine:    `#EXT-X-KEY:METHOD=ENCRYPTION,URI="https://placeholder.test/key?a=b=c"`,
			wantKeyURL: "https://placeholder.test/key?a=b=c",
		},
		{
			name:       "no URI",
			keyLine:    `#EXT-X-KEY:METHOD=ENCRYPTION`,
			wantKeyURL: "",
		},
		{
			name:       "empty URI",
			keyLine:    `#EXT-X-KEY:METHOD=ENCRYPTION,URI=""`,
			wantKeyURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.keyLine + "\n#EXTINF:1\nchunk.ts\n"
			scanner := bufio.NewScanner(strings.NewReader(input))
			result, err := client.ParsePlaylist(scanner, 1, "Test", 1)
			if err != nil {
				t.Fatalf("ParsePlaylist() unexpected error: %v", err)
			}

			if result.KeyURL != tt.wantKeyURL {
				t.Errorf("KeyURL = %q, want %q", result.KeyURL, tt.wantKeyURL)
			}
		})
	}
}

func TestParsePlaylistEmptyScanner(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	result, err := client.ParsePlaylist(scanner, 1, "Empty", 1)
	if err != nil {
		t.Fatalf("ParsePlaylist() unexpected error: %v", err)
	}

	if result.ID != 1 {
		t.Errorf("ID = %d, want 1", result.ID)
	}
	if result.Title != "Empty" {
		t.Errorf("Title = %q, want %q", result.Title, "Empty")
	}
	if result.SeqNo != 1 {
		t.Errorf("SeqNo = %d, want 1", result.SeqNo)
	}
	if result.KeyURL != "" {
		t.Errorf("KeyURL = %q, want empty", result.KeyURL)
	}
	if len(result.FirstViewURLs) != 0 {
		t.Errorf("FirstViewURLs len = %d, want 0", len(result.FirstViewURLs))
	}
	if result.HasMultipleViews {
		t.Errorf("HasMultipleViews = true, want false")
	}
}
