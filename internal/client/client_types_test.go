package client

import (
	"strings"
	"testing"
)

func TestLecturesReverse(t *testing.T) {
	lectures := Lectures{{SeqNo: 1}, {SeqNo: 2}, {SeqNo: 3}}
	reversed := lectures.Reverse()
	if len(reversed) != 3 || reversed[0].SeqNo != 3 || reversed[2].SeqNo != 1 {
		t.Fatalf("unexpected reversed order: %+v", reversed)
	}
	// original must be unchanged
	if lectures[0].SeqNo != 1 {
		t.Error("Reverse mutated the original slice")
	}
}

func TestLecturesFilterNoAudio(t *testing.T) {
	lectures := Lectures{{SeqNo: 1}, {SeqNo: 2, NoAudio: 1}, {SeqNo: 3}}
	filtered := lectures.FilterNoAudio()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 lectures, got %d", len(filtered))
	}
	for _, l := range filtered {
		if l.NoAudio == 1 {
			t.Error("FilterNoAudio retained a no-audio lecture")
		}
	}
}

func TestLecturesSelectRange(t *testing.T) {
	// SelectRange reverses first (chronological), then 1-based inclusive slice.
	lectures := Lectures{{SeqNo: 1}, {SeqNo: 2}, {SeqNo: 3}}

	got, err := lectures.SelectRange(1, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].SeqNo != 3 || got[1].SeqNo != 2 {
		t.Fatalf("unexpected range: %+v", got)
	}

	if _, err := lectures.SelectRange(2, 1); err == nil {
		t.Error("expected error for start > end")
	}
	if _, err := (Lectures{}).SelectRange(0, 0); err == nil {
		t.Error("expected error for empty lectures")
	}
}

func TestLecturesSelectForDownload(t *testing.T) {
	lectures := Lectures{
		{SeqNo: 1},
		{SeqNo: 2, NoAudio: 1},
		{SeqNo: 3},
		{SeqNo: 4, NoAudio: 1},
	}

	tests := []struct {
		name         string
		lectures     Lectures
		start        int
		end          int
		skipNoAudio  bool
		wantSeq      []int
		wantFiltered int
		wantErr      string
	}{
		{
			name:     "defaults select all in chronological order",
			lectures: lectures,
			wantSeq:  []int{4, 3, 2, 1},
		},
		{
			name:         "filters no-audio lectures and reports count",
			lectures:     lectures,
			skipNoAudio:  true,
			wantSeq:      []int{3, 1},
			wantFiltered: 2,
		},
		{
			name:         "applies range before filtering",
			lectures:     lectures,
			start:        2,
			end:          3,
			skipNoAudio:  true,
			wantSeq:      []int{3},
			wantFiltered: 1,
		},
		{
			name:     "returns range error",
			lectures: lectures,
			start:    3,
			end:      2,
			wantErr:  "invalid lecture range",
		},
		{
			name:    "returns empty input error",
			wantErr: "no lectures found",
		},
		{
			name:         "returns empty-after-filtering error and count",
			lectures:     Lectures{{SeqNo: 1, NoAudio: 1}, {SeqNo: 2, NoAudio: 1}},
			skipNoAudio:  true,
			wantFiltered: 2,
			wantErr:      "no lectures available after filtering",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, filtered, err := tt.lectures.SelectForDownload(tt.start, tt.end, tt.skipNoAudio)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("SelectForDownload() error = %v, want containing %q", err, tt.wantErr)
				}
				if got != nil {
					t.Fatalf("SelectForDownload() result = %+v, want nil on error", got)
				}
				if filtered != tt.wantFiltered {
					t.Fatalf("SelectForDownload() filtered = %d, want %d", filtered, tt.wantFiltered)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectForDownload() unexpected error: %v", err)
			}
			if filtered != tt.wantFiltered {
				t.Fatalf("SelectForDownload() filtered = %d, want %d", filtered, tt.wantFiltered)
			}
			if len(got) != len(tt.wantSeq) {
				t.Fatalf("SelectForDownload() len = %d, want %d: %+v", len(got), len(tt.wantSeq), got)
			}
			for i, seq := range tt.wantSeq {
				if got[i].SeqNo != seq {
					t.Fatalf("SelectForDownload()[%d].SeqNo = %d, want %d", i, got[i].SeqNo, seq)
				}
			}
		})
	}
}
