package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildVerifiesContainerOnCompletedFileDescriptor(t *testing.T) {
	tests := []struct {
		name      string
		container string
		header    []byte
	}{
		{name: "mp4", container: "mp4", header: []byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}},
		{name: "m4a", container: "m4a", header: []byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}},
		{name: "mkv", container: "mkv", header: []byte{0x1a, 0x45, 0xdf, 0xa3}},
		{name: "mp3", container: "mp3", header: []byte("ID3")},
		{name: "aac", container: "aac", header: []byte{0xff, 0xf1}},
		{name: "opus", container: "opus", header: []byte("OggS")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lecture."+test.container)
			if err := os.WriteFile(path, test.header, 0o600); err != nil {
				t.Fatal(err)
			}
			audioOnly := test.container != "mp4" && test.container != "mkv"
			role := "video"
			audioFormat := ""
			if audioOnly {
				role = "audio"
				audioFormat = test.container
			}
			_, err := Build(BuildInput{
				Lecture:   Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
				Selection: Selection{Views: "left", Quality: "720", AudioOnly: audioOnly, AudioFormat: audioFormat},
				Files: []FileSpec{{
					Path:            path,
					Role:            role,
					View:            "left",
					Container:       test.container,
					VerifyContainer: true,
				}},
				ProducedAt: time.Now(),
				Producer:   Producer{Name: "impartus", Version: "test"},
			})
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "stale.mp4")
	if err := os.WriteFile(path, []byte("unrelated data"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Build(BuildInput{
		Lecture:    Lecture{TTID: 4, InstituteID: 1, SubjectID: 2, SessionID: 3},
		Selection:  Selection{Views: "left", Quality: "720"},
		Files:      []FileSpec{{Path: path, Role: "video", View: "left", Container: "mp4", VerifyContainer: true}},
		ProducedAt: time.Now(),
		Producer:   Producer{Name: "impartus", Version: "test"},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match container") {
		t.Fatalf("Build() error = %v, want container mismatch", err)
	}
}
