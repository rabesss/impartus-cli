package client

import "testing"

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
