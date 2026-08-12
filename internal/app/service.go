// Package app exposes small orchestration services shared by the CLI and TUI.
package app

import (
	"context"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
)

type catalogClient interface {
	GetCourses(context.Context, *config.Config) (client.Courses, error)
	GetLectures(context.Context, *config.Config, client.Course) (client.Lectures, error)
}

type playbackStreams interface {
	FetchLecturePlaylists(context.Context, []client.Lecture) ([]client.ParsedPlaylist, error)
	StartPlaybackStream(context.Context, client.ParsedPlaylist) (downloader.PlaybackStream, error)
}

type managedPlayer interface {
	Load(context.Context, string) error
	WaitForEnd(context.Context) error
	Close(context.Context) error
	Events() <-chan player.Event
	Pause(context.Context, bool) error
	SeekRelative(context.Context, float64) error
	SetVolume(context.Context, float64) error
	SetMute(context.Context, bool) error
	SetSpeed(context.Context, float64) error
	CycleVideo(context.Context) error
}

type playerStarter func(context.Context, player.Options) (managedPlayer, error)

type playbackHistory interface {
	Playback(context.Context, string) (library.PlaybackState, bool, error)
	RecordPlayback(context.Context, library.PlaybackState) error
}

// Service coordinates existing Impartus API, stream proxy, and player layers.
// It owns no terminal, HTTP, subprocess, or persistence implementation itself.
type Service struct {
	config        *config.Config
	catalog       catalogClient
	streams       playbackStreams
	startPlayer   playerStarter
	playerOptions player.Options
	history       playbackHistory
}

// New creates the production application service.
func New(cfg *config.Config, apiClient *client.Client) *Service {
	return NewWithPlayerOptions(cfg, apiClient, player.Options{})
}

// NewWithPlayerOptions creates the production service with explicit supervised
// mpv options for TUI and automation callers.
func NewWithPlayerOptions(cfg *config.Config, apiClient *client.Client, options player.Options) *Service {
	media := downloader.New(cfg, apiClient)
	return newService(cfg, apiClient, media, func(ctx context.Context, options player.Options) (managedPlayer, error) {
		return player.Start(ctx, options)
	}, options)
}

// NewWithLibrary creates the production application service with durable
// playback resume support for TUI and automation callers.
func NewWithLibrary(cfg *config.Config, apiClient *client.Client, store *library.Store) *Service {
	return NewWithLibraryAndPlayerOptions(cfg, apiClient, store, player.Options{})
}

// NewWithLibraryAndPlayerOptions creates the full playback service with both
// durable resume history and explicit supervised-mpv options.
func NewWithLibraryAndPlayerOptions(cfg *config.Config, apiClient *client.Client, store *library.Store, options player.Options) *Service {
	media := downloader.New(cfg, apiClient)
	return newServiceWithHistory(cfg, apiClient, media, func(ctx context.Context, playerOptions player.Options) (managedPlayer, error) {
		return player.Start(ctx, playerOptions)
	}, options, store)
}

func newService(cfg *config.Config, catalog catalogClient, streams playbackStreams, start playerStarter, options ...player.Options) *Service {
	service := &Service{
		config:      cfg,
		catalog:     catalog,
		streams:     streams,
		startPlayer: start,
	}
	if len(options) > 0 {
		service.playerOptions = options[0]
	}
	return service
}

func newServiceWithHistory(
	cfg *config.Config,
	catalog catalogClient,
	streams playbackStreams,
	start playerStarter,
	options player.Options,
	history playbackHistory,
) *Service {
	service := newService(cfg, catalog, streams, start, options)
	service.history = history
	return service
}
