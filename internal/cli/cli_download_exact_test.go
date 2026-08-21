package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestExactTTIDRoutesOnlyRequestedScopedLecture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/subjects/67/lectures/8" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(client.Lectures{
			{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 7, SeqNo: 1, Topic: "Earlier row"},
			{InstituteID: 9, SubjectID: 999, SessionID: 8, TTID: 42, SeqNo: 2, Topic: "Wrong scope"},
			{InstituteID: 9, SubjectID: 67, SessionID: 99, TTID: 42, SeqNo: 3, Topic: "Wrong session"},
			{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 42, SeqNo: 99, Topic: "Requested exact row"},
		}); err != nil {
			t.Errorf("encode lecture response: %v", err)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BaseURL:          server.URL,
		Token:            "synthetic-test-token",
		DownloadLocation: t.TempDir(),
		Views:            "left",
		Quality:          "720",
	}
	apiClient := client.New(server.Client(), func() string { return "exact-ttid-test" })
	var downloaded client.Lectures
	var recorded []artifact.Manifest

	result, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42"},
		quietDownloadPresentation(),
		downloadExecutionDependencies{
			ensureFFmpeg: func() error { return nil },
			initClient: func(context.Context) (*config.Config, *client.Client, error) {
				return cfg, apiClient, nil
			},
			downloadLectures: func(_ context.Context, _ *config.Config, _ *client.Client, lectures client.Lectures, _ downloadPresentationOptions) (downloadResult, error) {
				downloaded = append(client.Lectures(nil), lectures...)
				lecture := lectures[0]
				return downloadResult{
					Status:       "completed",
					LectureCount: 1,
					Artifacts: []artifact.Manifest{{
						ArtifactID: "synthetic-exact-ttid",
						Lecture: artifact.Lecture{
							InstituteID: lecture.InstituteID,
							SubjectID:   lecture.SubjectID,
							SessionID:   lecture.SessionID,
							TTID:        lecture.TTID,
							SeqNo:       lecture.SeqNo,
						},
					}},
				}, nil
			},
			recordArtifacts: func(_ context.Context, manifests []artifact.Manifest) error {
				recorded = append([]artifact.Manifest(nil), manifests...)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("executeDownloadWithDependenciesContext() error = %v", err)
	}
	if len(downloaded) != 1 {
		t.Fatalf("downloaded = %+v, want one lecture", downloaded)
	}
	selected := downloaded[0]
	if selected.TTID != 42 || selected.SubjectID != 67 || selected.SessionID != 8 || selected.InstituteID != 4 || selected.SeqNo != 99 {
		t.Fatalf("selected lecture = %+v", selected)
	}
	if result.LectureCount != 1 || result.TotalLectures != 1 || result.FilteredCount != 0 || !result.LibraryRecorded {
		t.Fatalf("result = %+v", result)
	}
	if len(recorded) != 1 || recorded[0].Lecture.TTID != 42 || recorded[0].Lecture.SubjectID != 67 || recorded[0].Lecture.SessionID != 8 || recorded[0].Lecture.InstituteID != 4 {
		t.Fatalf("recorded artifacts = %+v", recorded)
	}
}

func TestExactTTIDRejectsForeignScopeBeforeDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(client.Lectures{
			{InstituteID: 9, SubjectID: 999, SessionID: 8, TTID: 42, Topic: "Wrong scope"},
		}); err != nil {
			t.Errorf("encode lecture response: %v", err)
		}
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, Token: "synthetic-test-token", Views: "left", Quality: "720"}
	apiClient := client.New(server.Client(), nil)
	downloaded := false
	_, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42"},
		quietDownloadPresentation(),
		downloadExecutionDependencies{
			ensureFFmpeg: func() error { return nil },
			initClient: func(context.Context) (*config.Config, *client.Client, error) {
				return cfg, apiClient, nil
			},
			downloadLectures: func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error) {
				downloaded = true
				return downloadResult{}, errors.New("download should not run")
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "no lecture found for ttid 42 in subject 67 session 8") {
		t.Fatalf("exact foreign-scope error = %v", err)
	}
	if downloaded {
		t.Fatal("foreign-scope exact selection reached download")
	}
}

func TestExactTTIDRejectsAmbiguousInstituteRowsBeforeDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(client.Lectures{
			{InstituteID: 4, SubjectID: 67, SessionID: 8, TTID: 42, Topic: "Institute four"},
			{InstituteID: 9, SubjectID: 67, SessionID: 8, TTID: 42, Topic: "Institute nine"},
		}); err != nil {
			t.Errorf("encode lecture response: %v", err)
		}
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, Token: "synthetic-test-token", Views: "left", Quality: "720"}
	apiClient := client.New(server.Client(), nil)
	downloaded := false
	_, err := executeDownloadWithDependenciesContext(
		context.Background(),
		[]string{"-s", "67", "-S", "8", "--ttid", "42"},
		quietDownloadPresentation(),
		downloadExecutionDependencies{
			ensureFFmpeg: func() error { return nil },
			initClient: func(context.Context) (*config.Config, *client.Client, error) {
				return cfg, apiClient, nil
			},
			downloadLectures: func(context.Context, *config.Config, *client.Client, client.Lectures, downloadPresentationOptions) (downloadResult, error) {
				downloaded = true
				return downloadResult{}, errors.New("download should not run")
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "multiple lectures found for ttid 42 in subject 67 session 8") {
		t.Fatalf("exact ambiguous-institute error = %v", err)
	}
	if downloaded {
		t.Fatal("ambiguous exact selection reached download")
	}
}
