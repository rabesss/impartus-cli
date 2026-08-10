package watch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/library"
)

type retryingCourseCatalog struct {
	watcher *Watcher
	source  CourseSource
}

func (catalog retryingCourseCatalog) GetCourses(ctx context.Context, cfg *config.Config) (client.Courses, error) {
	var courses client.Courses
	err := catalog.watcher.retry(ctx, func() error {
		var catalogErr error
		courses, catalogErr = catalog.source.GetCourses(ctx, cfg)
		return catalogErr
	})
	return courses, err
}

// artifactIDForLecture derives the complete logical identity from the scoped
// catalog lecture so committed work can be skipped before playlist resolution.
func (watcher *Watcher) artifactIDForLecture(target config.WatchTarget, lecture client.Lecture) (string, error) {
	if !scopeMatches(target.SubjectID, lecture.SubjectID) {
		return "", fmt.Errorf("subject scope mismatch for lecture %d", lecture.TTID)
	}
	if !scopeMatches(target.SessionID, lecture.SessionID) {
		return "", fmt.Errorf("session scope mismatch for lecture %d", lecture.TTID)
	}
	if lecture.TTID <= 0 {
		return "", errors.New("lecture ttid must be positive")
	}
	if lecture.InstituteID <= 0 {
		return "", errors.New("lecture institute scope must be positive")
	}
	return artifact.NewID(artifact.Identity{
		InstituteID: lecture.InstituteID,
		SubjectID:   lecture.SubjectID,
		SessionID:   lecture.SessionID,
		TTID:        lecture.TTID,
		AudioOnly:   watcher.cfg.AudioOnly,
		Views:       watcher.cfg.Views,
		Quality:     watcher.cfg.Quality,
		AudioFormat: watcher.cfg.AudioFormat,
	})
}

func (watcher *Watcher) resolveLecture(
	ctx context.Context,
	target config.WatchTarget,
	lecture client.Lecture,
) (client.Lecture, client.ParsedPlaylist, library.ExpectedArtifact, string, error) {
	if lecture.SubjectID == 0 {
		lecture.SubjectID = target.SubjectID
	}
	if lecture.SessionID == 0 {
		lecture.SessionID = target.SessionID
	}
	playlists, err := watcher.fetchPlaylists(ctx, lecture)
	if err != nil {
		return lecture, client.ParsedPlaylist{}, library.ExpectedArtifact{}, "", fmt.Errorf("resolve lecture playlist: %w", err)
	}
	if len(playlists) != 1 {
		return lecture, client.ParsedPlaylist{}, library.ExpectedArtifact{}, "", fmt.Errorf("expected one playlist for lecture %d, got %d", lecture.TTID, len(playlists))
	}
	lecture, playlist, scopeErr := normalizeScope(target, lecture, playlists[0])
	if scopeErr != nil {
		return lecture, playlist, library.ExpectedArtifact{}, "", scopeErr
	}
	playlist.Title = watchScopedTitle(lecture, playlist.Title)
	plan, planErr := downloader.PlanJoinResult(watcher.cfg, playlist)
	if planErr != nil {
		return lecture, playlist, library.ExpectedArtifact{}, "", planErr
	}
	expected := expectedArtifact(lecture, watcher.cfg, plan, watcher.options.Now().UTC())
	artifactID, identityErr := artifact.NewID(expectedIdentity(expected))
	if identityErr != nil {
		return lecture, playlist, expected, "", identityErr
	}
	return lecture, playlist, expected, artifactID, nil
}

func (watcher *Watcher) fetchPlaylists(ctx context.Context, lecture client.Lecture) ([]client.ParsedPlaylist, error) {
	var result []client.ParsedPlaylist
	err := watcher.retry(ctx, func() error {
		var err error
		result, err = watcher.producer.FetchLecturePlaylists(ctx, []client.Lecture{lecture})
		return err
	})
	return result, err
}

func normalizeScope(target config.WatchTarget, lecture client.Lecture, playlist client.ParsedPlaylist) (client.Lecture, client.ParsedPlaylist, error) {
	if lecture.TTID <= 0 || playlist.ID != lecture.TTID {
		return lecture, playlist, errors.New("playlist does not match its source lecture")
	}
	if !scopeMatches(target.SubjectID, lecture.SubjectID, playlist.SubjectID) {
		return lecture, playlist, fmt.Errorf("subject scope mismatch for lecture %d", lecture.TTID)
	}
	if !scopeMatches(target.SessionID, lecture.SessionID, playlist.SessionID) {
		return lecture, playlist, fmt.Errorf("session scope mismatch for lecture %d", lecture.TTID)
	}
	instituteID := firstPositive(lecture.InstituteID, playlist.InstituteID)
	if !scopeMatches(instituteID, lecture.InstituteID, playlist.InstituteID) {
		return lecture, playlist, fmt.Errorf("institute scope mismatch for lecture %d", lecture.TTID)
	}
	lecture.SubjectID, lecture.SessionID, lecture.InstituteID = target.SubjectID, target.SessionID, instituteID
	playlist.SubjectID, playlist.SessionID, playlist.InstituteID = lecture.SubjectID, lecture.SessionID, instituteID
	if lecture.SeqNo == 0 {
		lecture.SeqNo = playlist.SeqNo
	}
	if playlist.SeqNo == 0 {
		playlist.SeqNo = lecture.SeqNo
	}
	if strings.TrimSpace(lecture.Topic) == "" {
		lecture.Topic = playlist.Title
	}
	if strings.TrimSpace(playlist.Title) == "" {
		playlist.Title = lecture.Topic
	}
	return lecture, playlist, nil
}

func watchScopedTitle(lecture client.Lecture, title string) string {
	return fmt.Sprintf(
		"inst-%d_sub-%d_sess-%d_ttid-%d %s",
		lecture.InstituteID,
		lecture.SubjectID,
		lecture.SessionID,
		lecture.TTID,
		strings.TrimSpace(title),
	)
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func scopeMatches(expected int, values ...int) bool {
	if expected <= 0 {
		return false
	}
	for _, value := range values {
		if value > 0 && value != expected {
			return false
		}
	}
	return true
}
