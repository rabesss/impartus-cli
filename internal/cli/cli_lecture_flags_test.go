package cli

import (
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

func TestNormalizeViews(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "first", want: "left"},
		{in: " SECOND ", want: "right"},
		{in: " BOTH ", want: "both"},
		{in: " left ", want: "left"},
		{in: "custom", want: "custom"},
	}

	for _, tc := range cases {
		if got := config.NormalizeViews(tc.in); got != tc.want {
			t.Fatalf("NormalizeViews(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSelectLectureRangeValidAndInvalidRanges(t *testing.T) {
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "Lecture 1"},
		client.Lecture{SeqNo: 2, Topic: "Lecture 2"},
		client.Lecture{SeqNo: 3, Topic: "Lecture 3"},
	}

	selected, err := lectures.SelectRange(1, 2)
	if err != nil {
		t.Fatalf("expected valid range, got %v", err)
	}
	if len(selected) != 2 || selected[0].SeqNo != 3 || selected[1].SeqNo != 2 {
		t.Fatalf("unexpected selected range: %+v", selected)
	}

	all, err := lectures.SelectRange(0, 0)
	if err != nil {
		t.Fatalf("expected default range, got %v", err)
	}
	if len(all) != 3 || all[0].SeqNo != 3 || all[2].SeqNo != 1 {
		t.Fatalf("unexpected default range: %+v", all)
	}

	invalidCases := []struct {
		name     string
		lectures client.Lectures
		start    int
		end      int
		wantErr  string
	}{
		{name: "start greater than end", lectures: lectures, start: 3, end: 2, wantErr: "invalid lecture range"},
		{name: "end out of bounds", lectures: lectures, start: 1, end: 4, wantErr: "invalid lecture range"},
		{name: "no lectures", lectures: nil, start: 1, end: 1, wantErr: "no lectures found"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.lectures.SelectRange(tc.start, tc.end)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestFilterEmptyLecturesRemovesNoClassAndNoLectureVariants(t *testing.T) {
	lectures := client.Lectures{
		client.Lecture{Topic: "No class"},
		client.Lecture{Topic: "  no lecture  "},
		client.Lecture{Topic: "NO CLASS due to holiday"},
		client.Lecture{Topic: "There is no lecture today"},
		client.Lecture{Topic: "Regular Lecture"},
		client.Lecture{Topic: "Tutorial"},
	}

	filtered := filterEmptyLectures(lectures)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 lectures after filtering, got %d (%+v)", len(filtered), filtered)
	}
	if filtered[0].Topic != "Regular Lecture" || filtered[1].Topic != "Tutorial" {
		t.Fatalf("unexpected filtered lectures: %+v", filtered)
	}
}

func TestFilterNoAudioLectures(t *testing.T) {
	cases := []struct {
		name     string
		lectures client.Lectures
		wantLen  int
		wantErr  bool
	}{
		{
			name:     "all have audio",
			lectures: client.Lectures{client.Lecture{NoAudio: 0}, client.Lecture{NoAudio: 0}},
			wantLen:  2,
		},
		{
			name:     "all no audio",
			lectures: client.Lectures{client.Lecture{NoAudio: 1}, client.Lecture{NoAudio: 1}},
			wantLen:  0,
		},
		{
			name:     "mixed",
			lectures: client.Lectures{client.Lecture{NoAudio: 0}, client.Lecture{NoAudio: 1}, client.Lecture{NoAudio: 0}},
			wantLen:  2,
		},
		{
			name:     "single with audio",
			lectures: client.Lectures{client.Lecture{NoAudio: 0}},
			wantLen:  1,
		},
		{
			name:     "single no audio",
			lectures: client.Lectures{client.Lecture{NoAudio: 1}},
			wantLen:  0,
		},
		{
			name:     "empty list",
			lectures: client.Lectures{},
			wantLen:  0,
		},
		{
			name: "preserves other fields",
			lectures: client.Lectures{
				client.Lecture{SeqNo: 5, Topic: "Lecture 5", NoAudio: 0},
				client.Lecture{SeqNo: 10, Topic: "Lecture 10", NoAudio: 1},
			},
			wantLen: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filtered := tc.lectures.FilterNoAudio()
			if len(filtered) != tc.wantLen {
				t.Errorf("FilterNoAudio() len = %d, want %d", len(filtered), tc.wantLen)
			}
			// Verify audio lectures are preserved with original fields
			for _, lecture := range filtered {
				if lecture.NoAudio == 1 {
					t.Errorf("filtered list contains lecture with Noaudio=1")
				}
			}
		})
	}
}

func TestApplyLectureFilters(t *testing.T) {
	cases := []struct {
		name             string
		lectures         client.Lectures
		skipEmpty        bool
		skipNoAudio      bool
		wantFiltered     int
		wantEmptyCount   int
		wantNoAudioCount int
	}{
		{
			name: "no filtering",
			lectures: client.Lectures{
				client.Lecture{Topic: "Lecture 1", NoAudio: 0},
				client.Lecture{Topic: "Lecture 2", NoAudio: 0},
			},
			skipEmpty:        false,
			skipNoAudio:      false,
			wantFiltered:     2,
			wantEmptyCount:   0,
			wantNoAudioCount: 0,
		},
		{
			name: "filter empty only",
			lectures: client.Lectures{
				client.Lecture{Topic: "No class"},
				client.Lecture{Topic: "Lecture 1"},
				client.Lecture{Topic: "no lecture today"},
			},
			skipEmpty:        true,
			skipNoAudio:      false,
			wantFiltered:     1,
			wantEmptyCount:   2,
			wantNoAudioCount: 0,
		},
		{
			name: "filter noaudio only",
			lectures: client.Lectures{
				client.Lecture{Topic: "Lecture 1", NoAudio: 0},
				client.Lecture{Topic: "Lecture 2", NoAudio: 1},
				client.Lecture{Topic: "Lecture 3", NoAudio: 0},
			},
			skipEmpty:        false,
			skipNoAudio:      true,
			wantFiltered:     2,
			wantEmptyCount:   0,
			wantNoAudioCount: 1,
		},
		{
			name: "filter both empty and noaudio",
			lectures: client.Lectures{
				client.Lecture{Topic: "No class", NoAudio: 0},
				client.Lecture{Topic: "Lecture 1", NoAudio: 1},
				client.Lecture{Topic: "no lecture", NoAudio: 0},
				client.Lecture{Topic: "Lecture 2", NoAudio: 0},
			},
			skipEmpty:        true,
			skipNoAudio:      true,
			wantFiltered:     1, // Only "Lecture 2" remains (not empty, has audio)
			wantEmptyCount:   2, // "No class" and "no lecture" filtered as empty
			wantNoAudioCount: 1, // "Lecture 1" filtered as noaudio (after empty filter)
		},
		{
			name: "empty input",
			lectures: client.Lectures{
				client.Lecture{Topic: "No class", NoAudio: 1},
			},
			skipEmpty:        true,
			skipNoAudio:      true,
			wantFiltered:     0,
			wantEmptyCount:   1,
			wantNoAudioCount: 0, // Already empty after first filter
		},
		{
			name:             "nil input",
			lectures:         nil,
			skipEmpty:        true,
			skipNoAudio:      true,
			wantFiltered:     0,
			wantEmptyCount:   0,
			wantNoAudioCount: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filtered, emptyCount, noaudioCount := applyLectureFilters(tc.lectures, tc.skipEmpty, tc.skipNoAudio)
			if len(filtered) != tc.wantFiltered {
				t.Errorf("applyLectureFilters() filtered len = %d, want %d", len(filtered), tc.wantFiltered)
			}
			if emptyCount != tc.wantEmptyCount {
				t.Errorf("applyLectureFilters() emptyCount = %d, want %d", emptyCount, tc.wantEmptyCount)
			}
			if noaudioCount != tc.wantNoAudioCount {
				t.Errorf("applyLectureFilters() noaudioCount = %d, want %d", noaudioCount, tc.wantNoAudioCount)
			}
		})
	}
}

func TestFilterEmptyLectures(t *testing.T) {
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "Normal Lecture"},
		client.Lecture{SeqNo: 2, Topic: "No Class Today"},
		client.Lecture{SeqNo: 3, Topic: "No lecture scheduled"},
		client.Lecture{SeqNo: 4, Topic: "Advanced Topics"},
	}

	filtered := filterEmptyLectures(lectures)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 lectures after filtering, got %d", len(filtered))
	}
	if filtered[0].Topic != "Normal Lecture" {
		t.Errorf("first lecture topic = %q, want %q", filtered[0].Topic, "Normal Lecture")
	}
	if filtered[1].Topic != "Advanced Topics" {
		t.Errorf("second lecture topic = %q, want %q", filtered[1].Topic, "Advanced Topics")
	}
}

func TestFilterEmptyLectures_AllEmpty(t *testing.T) {
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "No Class"},
		client.Lecture{SeqNo: 2, Topic: "No Lecture Today"},
	}
	filtered := filterEmptyLectures(lectures)
	if len(filtered) != 0 {
		t.Errorf("expected 0 lectures, got %d", len(filtered))
	}
}

func TestFilterEmptyLectures_None(t *testing.T) {
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "Math"},
		client.Lecture{SeqNo: 2, Topic: "Physics"},
	}
	filtered := filterEmptyLectures(lectures)
	if len(filtered) != 2 {
		t.Errorf("expected 2 lectures, got %d", len(filtered))
	}
}

func TestAppendOutputPaths(t *testing.T) {
	result := downloader.JoinResult{
		LeftOutput:  "left.mp4",
		RightOutput: "",
		BothOutput:  "both.mp4",
	}

	paths := result.OutputPaths()
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}
	if paths[0] != "left.mp4" {
		t.Errorf("paths[0] = %q, want %q", paths[0], "left.mp4")
	}
	if paths[1] != "both.mp4" {
		t.Errorf("paths[1] = %q, want %q", paths[1], "both.mp4")
	}
}

func TestApplyAndValidateFlags_InvalidQuality(t *testing.T) {
	cfg := &config.Config{}
	_, err := applyAndValidateFlags(cfg, "99999", "both", false, "mp4", "", false)
	if err == nil {
		t.Error("expected error for invalid quality")
	}
}

func TestApplyAndValidateFlags_InvalidViews(t *testing.T) {
	cfg := &config.Config{}
	_, err := applyAndValidateFlags(cfg, "720", "invalid_view", false, "mp4", "", false)
	if err == nil {
		t.Error("expected error for invalid views")
	}
}

func TestApplyAndValidateFlags_InvalidFormat(t *testing.T) {
	cfg := &config.Config{}
	_, err := applyAndValidateFlags(cfg, "720", "both", true, "xyz", "", false)
	if err == nil {
		t.Error("expected error for invalid audio format")
	}
}

func TestApplyAndValidateFlags_ValidFlags(t *testing.T) {
	cfg := &config.Config{}
	result, err := applyAndValidateFlags(cfg, "720", "both", true, "mp3", "/tmp/out", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Quality != "720" {
		t.Errorf("Quality = %q, want %q", result.Quality, "720")
	}
	if !result.AudioOnly {
		t.Error("AudioOnly should be true")
	}
}
