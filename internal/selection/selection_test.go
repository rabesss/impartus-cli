package selection

import "testing"

func TestParseViewNormalizesAliases(t *testing.T) {
	t.Parallel()

	tests := map[string]View{
		"first":    ViewLeft,
		" SECOND ": ViewRight,
		"both":     ViewBoth,
	}
	for input, want := range tests {
		got, ok := ParseView(input)
		if !ok || got != want {
			t.Errorf("ParseView(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
}

func TestViewIncludes(t *testing.T) {
	t.Parallel()

	if !ViewBoth.Includes(ViewLeft) || !ViewBoth.Includes(ViewRight) {
		t.Fatal("both must include left and right")
	}
	if ViewLeft.Includes(ViewRight) || !ViewLeft.Includes(ViewLeft) {
		t.Fatal("left must include only left")
	}
}

func TestSelectionEnums(t *testing.T) {
	t.Parallel()

	if !ValidQuality("720") || ValidQuality(" 720 ") || ValidQuality("1080") {
		t.Fatal("quality allow-list mismatch")
	}
	if !ValidAudioFormat("m4a") || ValidAudioFormat(" M4A ") || ValidAudioFormat("wav") {
		t.Fatal("audio-format allow-list mismatch")
	}
}
