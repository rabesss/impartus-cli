package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	//nolint:errcheck // closing write end of pipe in test helper
	w.Close()
	os.Stdout = oldStdout

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("ReadAll: %v", readErr)
	}
	return string(out), runErr
}

func restoreCLIState(t *testing.T) {
	t.Helper()
	oldArgs := os.Args
	oldInteractive := runInteractiveFn
	oldCourses := runCoursesFn
	oldLectures := runLecturesFn
	oldDownload := runDownloadFn
	oldServe := runServeFn
	t.Cleanup(func() {
		os.Args = oldArgs
		runInteractiveFn = oldInteractive
		runCoursesFn = oldCourses
		runLecturesFn = oldLectures
		runDownloadFn = oldDownload
		runServeFn = oldServe
	})
}

func TestExecuteNoArgsDefaultsToInteractive(t *testing.T) {
	restoreCLIState(t)
	called := false
	runInteractiveFn = func() error {
		called = true
		return nil
	}
	os.Args = []string{"impartus"}

	if err := Execute("dev", ""); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected interactive mode to be used when no args are provided")
	}
}

func TestExecuteJSONNoSubcommandReturnsCapabilitiesEnvelope(t *testing.T) {
	restoreCLIState(t)
	runInteractiveFn = func() error {
		t.Fatal("interactive mode should not run in --json mode")
		return nil
	}
	os.Args = []string{"impartus", "--json"}

	output, err := captureStdout(t, func() error { return Execute("1.2.3", "2025-01-01") })
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Name        string `json:"name"`
			DefaultMode string `json:"defaultMode"`
			Flags       []string
			Commands    []struct {
				Name  string `json:"name"`
				Usage string `json:"usage"`
			} `json:"commands"`
		} `json:"data"`
		Error any `json:"error"`
		Meta  struct {
			Command string `json:"command"`
			Mode    string `json:"mode"`
		} `json:"meta"`
	}
	if unmarshalErr := json.Unmarshal([]byte(output), &payload); unmarshalErr != nil {
		t.Fatalf("failed to decode payload: %v; output=%q", unmarshalErr, output)
	}
	if !payload.Success || payload.Error != nil {
		t.Fatalf("expected successful envelope without error, got %+v", payload)
	}
	if payload.Meta.Command != "help" || payload.Meta.Mode != "json" {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	if payload.Data.DefaultMode != "interactive" || payload.Data.Name == "" || len(payload.Data.Commands) == 0 {
		t.Fatalf("unexpected capability payload: %+v", payload.Data)
	}
}

func TestExecuteJSONEnvelopeShapeForVersionAndErrors(t *testing.T) {
	restoreCLIState(t)
	cases := []struct {
		name       string
		args       []string
		expectErr  bool
		metaCmd    string
		errorMatch string
	}{
		{name: "json before command", args: []string{"impartus", "--json", "version"}, metaCmd: "version"},
		{name: "json after command", args: []string{"impartus", "version", "--json"}, metaCmd: "version"},
		{name: "unknown command", args: []string{"impartus", "unknown", "--json"}, expectErr: true, metaCmd: "unknown", errorMatch: "unknown command"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Args = tc.args
			output, err := captureStdout(t, func() error { return Execute("v1", "d1") })

			var raw string
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error")
				}
				raw = err.Error()
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				raw = output
			}

			var envelope map[string]json.RawMessage
			if unmarshalErr := json.Unmarshal([]byte(raw), &envelope); unmarshalErr != nil {
				t.Fatalf("invalid envelope json: %v; raw=%q", unmarshalErr, raw)
			}
			for _, key := range []string{"success", "data", "error", "meta"} {
				if _, ok := envelope[key]; !ok {
					t.Fatalf("missing envelope key %q in %v", key, envelope)
				}
			}

			var meta struct {
				Command string `json:"command"`
				Mode    string `json:"mode"`
			}
			if unmarshalErr := json.Unmarshal(envelope["meta"], &meta); unmarshalErr != nil {
				t.Fatalf("failed to parse meta: %v", unmarshalErr)
			}
			if meta.Command != tc.metaCmd || meta.Mode != "json" {
				t.Fatalf("unexpected meta: %+v", meta)
			}

			if tc.expectErr {
				var errPayload struct {
					Message string `json:"message"`
				}
				if unmarshalErr := json.Unmarshal(envelope["error"], &errPayload); unmarshalErr != nil {
					t.Fatalf("failed to parse error payload: %v", unmarshalErr)
				}
				if !strings.Contains(errPayload.Message, tc.errorMatch) {
					t.Fatalf("expected error message to contain %q, got %q", tc.errorMatch, errPayload.Message)
				}
			}
		})
	}
}

func TestExecuteJSONServeReturnsDeterministicMetadata(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "serve", "--port", "9090", "--json"}

	output, err := captureStdout(t, func() error { return Execute("v1", "d1") })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
			Port   int    `json:"port"`
			Base   string `json:"baseURL"`
			Health string `json:"health"`
		} `json:"data"`
		Meta struct {
			Command string `json:"command"`
			Mode    string `json:"mode"`
		} `json:"meta"`
	}
	if unmarshalErr := json.Unmarshal([]byte(output), &payload); unmarshalErr != nil {
		t.Fatalf("failed to decode payload: %v; output=%q", unmarshalErr, output)
	}
	if !payload.Success {
		t.Fatalf("expected success payload, got %+v", payload)
	}
	if payload.Meta.Command != "serve" || payload.Meta.Mode != "json" {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	if payload.Data.Status != "ready" || payload.Data.Port != 9090 {
		t.Fatalf("unexpected serve payload data: %+v", payload.Data)
	}
	if !strings.Contains(payload.Data.Base, "9090") || !strings.Contains(payload.Data.Health, "/health") {
		t.Fatalf("unexpected endpoint metadata: %+v", payload.Data)
	}
}

func TestExecuteJSONValidationAndDownloadEnvelope(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "download", "--json"}
	_, err := captureStdout(t, func() error { return Execute("v1", "d1") })
	if err == nil {
		t.Fatal("expected json envelope error")
	}

	var payload struct {
		Success bool `json:"success"`
		Data    any  `json:"data"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
		Meta struct {
			Command string `json:"command"`
			Mode    string `json:"mode"`
		} `json:"meta"`
	}
	if unmarshalErr := json.Unmarshal([]byte(err.Error()), &payload); unmarshalErr != nil {
		t.Fatalf("failed to decode error payload: %v; raw=%q", unmarshalErr, err.Error())
	}
	if payload.Success || payload.Data != nil {
		t.Fatalf("expected failed envelope with nil data, got %+v", payload)
	}
	if payload.Meta.Command != "download" || payload.Meta.Mode != "json" {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	if !strings.Contains(payload.Error.Message, "requires --subject/-s and --session/-S") {
		t.Fatalf("unexpected error message: %+v", payload.Error)
	}
}

func TestExecuteJSONDownloadUsesStructuredResult(t *testing.T) {
	result := downloadResult{Status: "completed", OutputPaths: []string{"/tmp/out.mp4"}, LectureCount: 1}
	payload := newSuccessEnvelope("download", result)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded struct {
		Success bool `json:"success"`
		Data    struct {
			Status       string   `json:"status"`
			OutputPaths  []string `json:"outputPaths"`
			LectureCount int      `json:"lectureCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Success || decoded.Data.Status != "completed" || len(decoded.Data.OutputPaths) != 1 || decoded.Data.LectureCount != 1 {
		t.Fatalf("unexpected decoded payload: %+v", decoded)
	}
}

func TestCoursesRejectsPositionalArguments(t *testing.T) {
	_, err := getCourses([]string{"extra"})
	if err == nil {
		t.Fatal("expected positional argument rejection")
	}
	if !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVersionRejectsPositionalArgumentsJSON(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "version", "extra", "--json"}

	err := Execute("v1", "d1")
	if err == nil {
		t.Fatal("expected positional argument rejection error")
	}
	raw := err.Error()
	if !strings.Contains(raw, "version does not accept positional arguments") {
		t.Fatalf("expected error message about positional arguments, got: %v", err)
	}
	// Verify it's a proper JSON envelope
	var envelope map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal([]byte(raw), &envelope); unmarshalErr != nil {
		t.Fatalf("expected JSON envelope, got raw text: %v", err)
	}
}

func TestServeRejectsPositionalArgumentsJSON(t *testing.T) {
	restoreCLIState(t)
	os.Args = []string{"impartus", "serve", "extra", "--json"}

	err := Execute("v1", "d1")
	if err == nil {
		t.Fatal("expected positional argument rejection error")
	}
	raw := err.Error()
	if !strings.Contains(raw, "serve does not accept positional arguments") {
		t.Fatalf("expected error message about positional arguments, got: %v", err)
	}
	// Verify it's a proper JSON envelope
	var envelope map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal([]byte(raw), &envelope); unmarshalErr != nil {
		t.Fatalf("expected JSON envelope, got raw text: %v", err)
	}
}

func TestGetLecturesRejectsPositionalArguments(t *testing.T) {
	_, err := getLectures([]string{"--subject", "1", "--session", "2", "extra"})
	if err == nil {
		t.Fatal("expected positional argument rejection")
	}
	if !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

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

func TestValidateFlagOverrides(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid quality 144",
			cfg:     &config.Config{Quality: "144"},
			wantErr: false,
		},
		{
			name:    "valid quality 450",
			cfg:     &config.Config{Quality: "450"},
			wantErr: false,
		},
		{
			name:    "valid quality 720",
			cfg:     &config.Config{Quality: "720"},
			wantErr: false,
		},
		{
			name:    "invalid quality 1080",
			cfg:     &config.Config{Quality: "1080"},
			wantErr: true,
			errMsg:  "invalid quality value \"1080\"",
		},
		{
			name:    "invalid quality hd",
			cfg:     &config.Config{Quality: "hd"},
			wantErr: true,
			errMsg:  "invalid quality value \"hd\"",
		},
		{
			name:    "valid views first",
			cfg:     &config.Config{Views: "first"},
			wantErr: false,
		},
		{
			name:    "valid views second",
			cfg:     &config.Config{Views: "second"},
			wantErr: false,
		},
		{
			name:    "valid views both",
			cfg:     &config.Config{Views: "both"},
			wantErr: false,
		},
		{
			name:    "valid views left (legacy)",
			cfg:     &config.Config{Views: "left"},
			wantErr: false,
		},
		{
			name:    "valid views right (legacy)",
			cfg:     &config.Config{Views: "right"},
			wantErr: false,
		},
		{
			name:    "invalid views sideways",
			cfg:     &config.Config{Views: "sideways"},
			wantErr: true,
			errMsg:  "invalid views value \"sideways\"",
		},
		{
			name:    "valid audio format mp3",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "mp3"},
			wantErr: false,
		},
		{
			name:    "valid audio format m4a",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "m4a"},
			wantErr: false,
		},
		{
			name:    "valid audio format aac",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "aac"},
			wantErr: false,
		},
		{
			name:    "valid audio format opus",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "opus"},
			wantErr: false,
		},
		{
			name:    "invalid audio format wav",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: "wav"},
			wantErr: true,
			errMsg:  "invalid audioFormat value \"wav\"",
		},
		{
			name:    "empty config is valid",
			cfg:     &config.Config{},
			wantErr: false,
		},
		{
			name:    "empty quality is valid",
			cfg:     &config.Config{Quality: ""},
			wantErr: false,
		},
		{
			name:    "empty views is valid",
			cfg:     &config.Config{Views: ""},
			wantErr: false,
		},
		{
			name:    "empty audio format is valid when audio-only",
			cfg:     &config.Config{AudioOnly: true, AudioFormat: ""},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFlagOverrides(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				if !strings.Contains(err.Error(), tc.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tc.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
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

	paths := appendOutputPaths(nil, result)
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

func TestAppendOutputPaths_AllEmpty(t *testing.T) {
	result := downloader.JoinResult{}
	paths := appendOutputPaths(nil, result)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
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
