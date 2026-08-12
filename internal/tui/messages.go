package tui

import (
	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
)

type lifecycleCanceledMsg struct{}
type fatalOperationMsg struct{}

type coursesLoadedMsg struct {
	courses client.Courses
	err     error
}

type lecturesLoadedMsg struct {
	course   client.Course
	lectures client.Lectures
	err      error
}

type resumeLoadedMsg struct {
	lecture client.Lecture
	state   library.PlaybackState
	found   bool
	err     error
}

type playbackStartedMsg struct {
	lecture       client.Lecture
	resume        library.PlaybackState
	playback      app.PlaybackSession
	lease         uint64
	initialEvents []player.Event
	err           error
}

type playbackControlMsg struct {
	generation uint64
	action     string
	value      float64
	flag       bool
	err        error
}

type playbackEventMsg struct {
	generation uint64
	event      player.Event
	open       bool
	canceled   bool
}

type playbackFinishedMsg struct {
	generation uint64
	state      library.PlaybackState
	err        error
}

type downloadFinishedMsg struct {
	lecture client.Lecture
	result  app.DownloadResult
	err     error
}

type artifactsLoadedMsg struct {
	artifacts []library.ArtifactRecord
	err       error
}
