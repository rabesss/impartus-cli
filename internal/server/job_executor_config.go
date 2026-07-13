package server

import (
	"errors"
	"strings"

	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/paths"
)

func mergeConfigWithJobOptions(globalCfg *config.Config, opts *JobConfigOptions) (*config.Config, error) {
	cfg := cloneConfig(globalCfg)
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.ApplyDefaults()

	if err := applyJobConfigOverrides(cfg, opts); err != nil {
		return nil, err
	}

	if cfg.DownloadLocation == "" {
		return nil, errors.New("outputPath/downloadLocation cannot be empty")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func runtimeConfigFrom(cfg *config.Config) JobRuntimeConfig {
	if cfg == nil {
		return JobRuntimeConfig{}
	}
	return JobRuntimeConfig{
		Quality:                   cfg.Quality,
		Views:                     config.NormalizeViews(cfg.Views),
		AudioOnly:                 cfg.AudioOnly,
		AudioFormat:               cfg.AudioFormat,
		OutputPath:                cfg.DownloadLocation,
		EnablePipeline:            cfg.EnablePipeline,
		NumWorkers:                cfg.NumWorkers,
		DownloadWorkersPerLecture: cfg.DownloadWorkersPerLecture,
		DecryptWorkersPerLecture:  cfg.DecryptWorkersPerLecture,
		Slides:                    cfg.Slides,
		SkipNoAudio:               cfg.SkipNoAudio,
	}
}

func cloneConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	return &clone
}

func applyOutputPathOverride(cfg *config.Config, outputPath *string) error {
	if outputPath == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*outputPath)
	if trimmed == "" {
		return nil
	}
	// API overrides are untrusted: reject absolute paths and traversal escapes
	// instead of silently discarding them (the caller surfaces the error).
	location, err := paths.ValidateDownloadLocation(trimmed, false)
	if err != nil {
		return err
	}
	cfg.DownloadLocation = location
	return nil
}

func applyJobConfigOverrides(cfg *config.Config, opts *JobConfigOptions) error {
	if opts == nil {
		return nil
	}
	if opts.Quality != nil {
		cfg.Quality = *opts.Quality
	}
	if opts.Views != nil {
		cfg.Views = config.NormalizeViews(*opts.Views)
	}
	if opts.AudioOnly != nil {
		cfg.AudioOnly = *opts.AudioOnly
	}
	if opts.AudioFormat != nil {
		cfg.AudioFormat = *opts.AudioFormat
	}
	if err := applyOutputPathOverride(cfg, opts.OutputPath); err != nil {
		return err
	}
	if opts.EnablePipeline != nil {
		cfg.EnablePipeline = *opts.EnablePipeline
	}
	if opts.NumWorkers != nil {
		cfg.NumWorkers = *opts.NumWorkers
	}
	if opts.DownloadWorkersPerLecture != nil {
		cfg.DownloadWorkersPerLecture = *opts.DownloadWorkersPerLecture
	}
	if opts.DecryptWorkersPerLecture != nil {
		cfg.DecryptWorkersPerLecture = *opts.DecryptWorkersPerLecture
	}
	if opts.SkipNoAudio != nil {
		cfg.SkipNoAudio = *opts.SkipNoAudio
	}
	return nil
}
