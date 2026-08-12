package artifact

import (
	"encoding/hex"
	"testing"
)

func TestIdentityGoldenVector(t *testing.T) {
	identity := Identity{
		InstituteID: 4,
		SubjectID:   67,
		SessionID:   8,
		TTID:        12345,
		Views:       "both",
		Quality:     "720",
	}

	canonical, err := CanonicalBytes(identity)
	if err != nil {
		t.Fatalf("CanonicalBytes() error = %v", err)
	}
	const wantCanonicalHex = "696d7061727475732d61727469666163742d7631000000000000000004000000000000004300000000000000080000000000003039000004626f746800033732300000"
	if got := hex.EncodeToString(canonical); got != wantCanonicalHex {
		t.Fatalf("CanonicalBytes() = %s, want %s", got, wantCanonicalHex)
	}

	id, err := NewID(identity)
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	const wantID = "impartus:v1:CmQ1iLsQw_Aarxg3Rp4svvDdKX4sJ6R0KFWXn3keTn4"
	if id != wantID {
		t.Fatalf("NewID() = %q, want %q", id, wantID)
	}
}

func TestIdentityAudioGoldenVector(t *testing.T) {
	identity := Identity{
		InstituteID: 4,
		SubjectID:   67,
		SessionID:   8,
		TTID:        12345,
		AudioOnly:   true,
		Views:       "right",
		Quality:     "450",
		AudioFormat: "mp3",
	}

	canonical, err := CanonicalBytes(identity)
	if err != nil {
		t.Fatalf("CanonicalBytes() error = %v", err)
	}
	const wantCanonicalHex = "696d7061727475732d61727469666163742d76310000000000000000040000000000000043000000000000000800000000000030390100057269676874000334353000036d7033"
	if got := hex.EncodeToString(canonical); got != wantCanonicalHex {
		t.Fatalf("CanonicalBytes() = %s, want %s", got, wantCanonicalHex)
	}

	id, err := NewID(identity)
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	const wantID = "impartus:v1:IeKFWkvGsIoG0eyBLkNNEg6Ddigcskn0AONuJTNlQIw"
	if id != wantID {
		t.Fatalf("NewID() = %q, want %q", id, wantID)
	}
}

func TestIdentityNormalizationAndDimensions(t *testing.T) {
	base := Identity{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12345, Views: "left", Quality: "720"}
	baseID, err := NewID(base)
	if err != nil {
		t.Fatalf("NewID(base) error = %v", err)
	}

	aliases := []Identity{
		{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12345, Views: " first ", Quality: " 720 ", AudioFormat: "mp3"},
		{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12345, Views: "LEFT", Quality: "720"},
	}
	for _, alias := range aliases {
		got, aliasErr := NewID(alias)
		if aliasErr != nil {
			t.Fatalf("NewID(alias) error = %v", aliasErr)
		}
		if got != baseID {
			t.Fatalf("normalized ID = %q, want %q", got, baseID)
		}
	}

	changes := []Identity{
		{InstituteID: 5, SubjectID: 67, SessionID: 8, TTID: 12345, Views: "left", Quality: "720"},
		{InstituteID: 4, SubjectID: 68, SessionID: 8, TTID: 12345, Views: "left", Quality: "720"},
		{InstituteID: 4, SubjectID: 67, SessionID: 9, TTID: 12345, Views: "left", Quality: "720"},
		{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12346, Views: "left", Quality: "720"},
		{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12345, Views: "right", Quality: "720"},
		{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12345, Views: "left", Quality: "450"},
		{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 12345, AudioOnly: true, Views: "left", Quality: "720", AudioFormat: "mp3"},
	}
	for _, changed := range changes {
		got, changedErr := NewID(changed)
		if changedErr != nil {
			t.Fatalf("NewID(changed) error = %v", changedErr)
		}
		if got == baseID {
			t.Fatalf("changed identity %+v retained base ID", changed)
		}
	}
}

func TestIdentityRejectsInvalidValues(t *testing.T) {
	valid := Identity{InstituteID: 1, SubjectID: 2, SessionID: 3, TTID: 4, Views: "both", Quality: "720"}
	tests := []struct {
		name     string
		identity Identity
	}{
		{name: "non-positive ID", identity: Identity{Views: "both", Quality: "720"}},
		{name: "views", identity: withIdentityChange(valid, func(value *Identity) { value.Views = "sideways" })},
		{name: "quality", identity: withIdentityChange(valid, func(value *Identity) { value.Quality = "1080" })},
		{name: "missing audio format", identity: withIdentityChange(valid, func(value *Identity) { value.AudioOnly = true })},
		{name: "invalid audio format", identity: withIdentityChange(valid, func(value *Identity) { value.AudioOnly = true; value.AudioFormat = "wav" })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewID(test.identity); err == nil {
				t.Fatal("NewID() error = nil, want validation error")
			}
		})
	}
}

func withIdentityChange(identity Identity, change func(*Identity)) Identity {
	change(&identity)
	return identity
}
