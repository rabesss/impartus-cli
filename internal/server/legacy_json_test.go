package server

import (
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestMergeConfigWithJobOptionsAppliesOverridesAndValidatesInvalidValues(t *testing.T) {
	cfg := validServerConfig()
	opts := &JobConfigOptions{
		Quality:                   strPtr("720"),
		Views:                     strPtr("second"),
		AudioOnly:                 boolPtr(true),
		AudioFormat:               strPtr("aac"),
		OutputPath:                strPtr(" ./custom-output "),
		EnablePipeline:            boolPtr(true),
		NumWorkers:                intPtr(8),
		DownloadWorkersPerLecture: intPtr(4),
		DecryptWorkersPerLecture:  intPtr(2),
	}

	merged, err := mergeConfigWithJobOptions(cfg, opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if merged.Quality != "720" || merged.Views != "right" || !merged.AudioOnly || merged.AudioFormat != "aac" {
		t.Fatalf("unexpected merged media config: %+v", merged)
	}
	if merged.DownloadLocation != "custom-output" {
		t.Fatalf("expected trimmed output path override, got %q", merged.DownloadLocation)
	}
	if !merged.EnablePipeline || merged.NumWorkers != 8 || merged.DownloadWorkersPerLecture != 4 || merged.DecryptWorkersPerLecture != 2 {
		t.Fatalf("unexpected merged worker config: %+v", merged)
	}
	if cfg.Quality != "450" {
		t.Fatalf("expected original config to remain unchanged, got quality=%q", cfg.Quality)
	}

	invalidCases := []struct {
		name    string
		opts    *JobConfigOptions
		wantErr string
	}{
		{name: "invalid quality", opts: &JobConfigOptions{Quality: strPtr("1080")}, wantErr: "quality must be one of"},
		{name: "invalid workers", opts: &JobConfigOptions{NumWorkers: intPtr(0)}, wantErr: "numWorkers must be between"},
		{name: "empty output", opts: &JobConfigOptions{OutputPath: strPtr("   ")}, wantErr: ""},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mergeConfigWithJobOptions(validServerConfig(), tc.opts)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestCreateJobRequestEffectiveJobConfigMapsTopLevelFields(t *testing.T) {
	quality := "450"
	views := "both"
	audioOnly := true
	audioFormat := "opus"
	outputPath := "./output"
	enablePipeline := true
	numWorkers := 7
	downloadWorkers := 3
	decryptWorkers := 2

	req := createJobRequest{
		JobConfig: &JobConfigOptions{
			Quality:                   &quality,
			Views:                     &views,
			AudioOnly:                 &audioOnly,
			AudioFormat:               &audioFormat,
			OutputPath:                &outputPath,
			EnablePipeline:            &enablePipeline,
			NumWorkers:                &numWorkers,
			DownloadWorkersPerLecture: &downloadWorkers,
			DecryptWorkersPerLecture:  &decryptWorkers,
		},
	}

	effective := req.effectiveJobConfig()
	if effective == nil {
		t.Fatal("expected effective config, got nil")
	}

	if *effective.Quality != quality || *effective.Views != views || *effective.AudioOnly != audioOnly ||
		*effective.AudioFormat != audioFormat || *effective.OutputPath != outputPath ||
		*effective.EnablePipeline != enablePipeline || *effective.NumWorkers != numWorkers ||
		*effective.DownloadWorkersPerLecture != downloadWorkers || *effective.DecryptWorkersPerLecture != decryptWorkers {
		t.Fatalf("unexpected effective config mapping: %+v", effective)
	}
}

func TestNormalizeViewsViaConfig(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "first", want: "left"},
		{in: "second", want: "right"},
		{in: "both", want: "both"},
		{in: "left", want: "left"},
		{in: "", want: ""},
	}

	for _, tc := range cases {
		if got := config.NormalizeViews(tc.in); got != tc.want {
			t.Fatalf("NormalizeViews(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSelectJobLecturesMatchesCLIAlignment verifies that selectJobLectures
// produces the same lecture selection as CLI's selectLectureRange for identical
// index inputs. This ensures VAL-CROSS-002: CLI range selection and API job ranges
// refer to the same lecture slice.
func TestSelectJobLecturesMatchesCLIAlignment(t *testing.T) {
	// Create test lectures matching the structure used in CLI tests
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "Lecture 1"},
		client.Lecture{SeqNo: 2, Topic: "Lecture 2"},
		client.Lecture{SeqNo: 3, Topic: "Lecture 3"},
		client.Lecture{SeqNo: 4, Topic: "Lecture 4"},
		client.Lecture{SeqNo: 5, Topic: "Lecture 5"},
	}

	// Test case 1: range 1-2 should select first 2 lectures from reversed order
	// CLI's selectLectureRange: reverses to [5,4,3,2,1], then takes [5,4] for range 1-2
	job1 := &Job{StartIndex: 1, EndIndex: 2}
	selected1, filtered1, err := selectJobLectures(job1, lectures)
	if err != nil {
		t.Fatalf("selectJobLectures(1-2) unexpected error: %v", err)
	}
	if filtered1 != 0 {
		t.Fatalf("expected 0 filtered lectures, got %d", filtered1)
	}
	// Expected: reversed [5,4,3,2,1], take indices 0-1 => [5,4]
	if len(selected1) != 2 {
		t.Fatalf("expected 2 lectures, got %d", len(selected1))
	}
	if selected1[0].SeqNo != 5 || selected1[1].SeqNo != 4 {
		t.Errorf("range 1-2: expected [5, 4], got [%d, %d]", selected1[0].SeqNo, selected1[1].SeqNo)
	}

	// Test case 2: range 1-5 (full range) should select all lectures in reverse order
	job2 := &Job{StartIndex: 1, EndIndex: 5}
	selected2, filtered2, err := selectJobLectures(job2, lectures)
	if err != nil {
		t.Fatalf("selectJobLectures(1-5) unexpected error: %v", err)
	}
	if filtered2 != 0 {
		t.Fatalf("expected 0 filtered lectures, got %d", filtered2)
	}
	expectedSeqNos := []int{5, 4, 3, 2, 1}
	if len(selected2) != len(expectedSeqNos) {
		t.Fatalf("expected %d lectures, got %d", len(expectedSeqNos), len(selected2))
	}
	for i, expected := range expectedSeqNos {
		if selected2[i].SeqNo != expected {
			t.Errorf("full range: position %d expected SeqNo %d, got %d", i, expected, selected2[i].SeqNo)
		}
	}

	// Test case 3: default range (0,0) should select all lectures
	job3 := &Job{StartIndex: 0, EndIndex: 0}
	selected3, filtered3, err := selectJobLectures(job3, lectures)
	if err != nil {
		t.Fatalf("selectJobLectures(0,0) unexpected error: %v", err)
	}
	if filtered3 != 0 {
		t.Fatalf("expected 0 filtered lectures, got %d", filtered3)
	}
	if len(selected3) != 5 {
		t.Errorf("default range: expected 5 lectures, got %d", len(selected3))
	}
	// Default should also be reversed
	for i, expected := range expectedSeqNos {
		if selected3[i].SeqNo != expected {
			t.Errorf("default range: position %d expected SeqNo %d, got %d", i, expected, selected3[i].SeqNo)
		}
	}

	// Test case 4: range 3-4 should select middle lectures from reversed order
	// Reversed: [5,4,3,2,1], indices 2-3 => [3,2]
	job4 := &Job{StartIndex: 3, EndIndex: 4}
	selected4, filtered4, err := selectJobLectures(job4, lectures)
	if err != nil {
		t.Fatalf("selectJobLectures(3-4) unexpected error: %v", err)
	}
	if filtered4 != 0 {
		t.Fatalf("expected 0 filtered lectures, got %d", filtered4)
	}
	if len(selected4) != 2 {
		t.Fatalf("expected 2 lectures, got %d", len(selected4))
	}
	if selected4[0].SeqNo != 3 || selected4[1].SeqNo != 2 {
		t.Errorf("range 3-4: expected [3, 2], got [%d, %d]", selected4[0].SeqNo, selected4[1].SeqNo)
	}
}

// TestSelectJobLecturesOutOfRange validates error handling for invalid ranges

// TestSelectJobLecturesOutOfRange validates error handling for invalid ranges
func TestSelectJobLecturesOutOfRange(t *testing.T) {
	lectures := client.Lectures{
		client.Lecture{SeqNo: 1, Topic: "Lecture 1"},
		client.Lecture{SeqNo: 2, Topic: "Lecture 2"},
	}

	// Start index beyond available lectures
	job := &Job{StartIndex: 5, EndIndex: 10}
	_, _, err := selectJobLectures(job, lectures)
	if err == nil {
		t.Error("expected error for startIndex out of range")
	}
	if !strings.Contains(err.Error(), "range") {
		t.Errorf("expected 'range' error, got: %v", err)
	}

	// Empty lectures
	emptyJob := &Job{StartIndex: 1, EndIndex: 1}
	_, _, err = selectJobLectures(emptyJob, nil)
	if err == nil {
		t.Error("expected error for empty lectures")
	}
	if !strings.Contains(err.Error(), "no lectures found") {
		t.Errorf("expected 'no lectures found' error, got: %v", err)
	}
}
