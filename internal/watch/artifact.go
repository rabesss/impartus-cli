package watch

import (
	"path/filepath"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/buildinfo"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/library"
)

func expectedArtifact(lecture client.Lecture, cfg *config.Config, plan downloader.JoinResult, producedAt time.Time) library.ExpectedArtifact {
	role := "video"
	if cfg.AudioOnly {
		role = "audio"
	}
	files := make([]library.ExpectedFile, 0, 3)
	for _, output := range plan.Outputs() {
		files = append(files, library.ExpectedFile{Path: output.Path, Role: role, View: output.View, Container: output.Container})
	}
	return library.ExpectedArtifact{
		Lecture: artifact.Lecture{
			TTID: lecture.TTID, InstituteID: lecture.InstituteID, SubjectID: lecture.SubjectID,
			SessionID: lecture.SessionID, SeqNo: lecture.SeqNo, Topic: lecture.Topic,
			StartTime: lecture.StartTime, DurationSeconds: lecture.ActualDuration,
			Professor: lecture.ProfessorName, Institute: lecture.Institute, NoAudio: lecture.NoAudio == 1,
		},
		Selection: artifact.Selection{Views: cfg.Views, Quality: cfg.Quality, AudioOnly: cfg.AudioOnly, AudioFormat: cfg.AudioFormat},
		Files:     files, ProducedAt: producedAt, Producer: artifact.Producer{Name: "impartus", Version: buildinfo.Version},
	}
}

func expectedIdentity(expected library.ExpectedArtifact) artifact.Identity {
	return artifact.Identity{
		InstituteID: expected.Lecture.InstituteID, SubjectID: expected.Lecture.SubjectID,
		SessionID: expected.Lecture.SessionID, TTID: expected.Lecture.TTID,
		AudioOnly: expected.Selection.AudioOnly, Views: expected.Selection.Views,
		Quality: expected.Selection.Quality, AudioFormat: expected.Selection.AudioFormat,
	}
}

func manifestFromExpected(expected library.ExpectedArtifact, joined downloader.JoinResult) (artifact.Manifest, error) {
	role := "video"
	if expected.Selection.AudioOnly {
		role = "audio"
	}
	files := make([]artifact.FileSpec, 0, 3)
	for _, output := range joined.Outputs() {
		files = append(files, artifact.FileSpec{Path: output.Path, Role: role, View: output.View, Container: output.Container})
	}
	return artifact.Build(artifact.BuildInput{
		Lecture: expected.Lecture, Selection: expected.Selection, Files: files,
		ProducedAt: expected.ProducedAt, Producer: expected.Producer,
	})
}

func expectedPathsEqual(left, right library.ExpectedArtifact) bool {
	if len(left.Files) != len(right.Files) || left.Selection != right.Selection {
		return false
	}
	for index := range left.Files {
		leftPath, leftErr := filepath.Abs(filepath.Clean(left.Files[index].Path))
		rightPath, rightErr := filepath.Abs(filepath.Clean(right.Files[index].Path))
		if leftErr != nil || rightErr != nil {
			return false
		}
		if leftPath != rightPath || left.Files[index].Role != right.Files[index].Role ||
			left.Files[index].View != right.Files[index].View || left.Files[index].Container != right.Files[index].Container {
			return false
		}
	}
	return true
}

func manifestPaths(manifest artifact.Manifest) []string {
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	return paths
}
