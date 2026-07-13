// Package cli implements the command-line interface for the Impartus downloader.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

var (
	runInteractiveFn  = runInteractive
	runCoursesFn      = runCourses
	runLecturesFn     = runLectures
	runDownloadFn     = runDownload
	runDownloadJSONFn = runDownloadJSON
	runServeFn        = runServe
	runPlayFn         = runPlay
	loadResolvedFn    = config.LoadResolved
	newLoggedInFn     = client.NewLoggedIn
)

// Execute runs the root CLI command with the given version and build date.
func Execute(version, date string) error {
	args, jsonMode := stripGlobalJSONFlag(os.Args[1:])
	if len(args) == 0 {
		if jsonMode {
			return emitJSONEnvelope(newSuccessEnvelope("help", helpPayload()))
		}
		return runInteractiveFn()
	}

	if jsonMode {
		return executeJSON(args, version, date)
	}

	switch args[0] {
	case "version", "--version", "-version", "-v":
		if len(args) > 1 {
			return fmt.Errorf("version does not accept positional arguments")
		}
		showVersion(version, date)
		return nil
	case "help", "--help", "-help", "-h":
		showHelp(version, date)
		return nil
	case "courses":
		return runCoursesFn(args[1:])
	case "lectures":
		return runLecturesFn(args[1:])
	case "download":
		return runDownloadFn(args[1:])
	case "serve":
		return runServeFn(args[1:], version)
	case "play":
		return runPlayFn(args[1:])
	default:
		showHelp(version, date)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func executeJSON(args []string, version, date string) error {
	command := args[0]
	switch command {
	case "version", "--version", "-version", "-v":
		if len(args) > 1 {
			return newJSONError("version", fmt.Errorf("version does not accept positional arguments"))
		}
		return emitJSONEnvelope(newSuccessEnvelope("version", versionPayload{
			Name:      "impartus",
			Version:   version,
			BuildDate: date,
		}))
	case "help", "--help", "-help", "-h":
		return emitJSONEnvelope(newSuccessEnvelope("help", helpPayload()))
	case "courses":
		courses, err := getCourses(args[1:])
		if err != nil {
			return newJSONError("courses", err)
		}
		return emitJSONEnvelope(newSuccessEnvelope("courses", courses))
	case "lectures":
		lectures, err := getLectures(args[1:])
		if err != nil {
			return newJSONError("lectures", err)
		}
		return emitJSONEnvelope(newSuccessEnvelope("lectures", lectures))
	case "download":
		result, err := runDownloadJSONFn(args[1:])
		if err != nil {
			return newJSONError("download", err)
		}
		return emitJSONEnvelope(newSuccessEnvelope("download", result))
	case "play":
		return newJSONError("play", fmt.Errorf("play command is not supported in JSON mode"))
	case "serve":
		port, err := parseServePort(args[1:])
		if err != nil {
			return newJSONError("serve", err)
		}
		baseURL := fmt.Sprintf("http://localhost:%d/api/v1", port)
		return emitJSONEnvelope(newSuccessEnvelope("serve", map[string]any{
			"status":  "ready",
			"port":    port,
			"baseURL": baseURL,
			"health":  baseURL + "/health",
			"note":    "json mode is non-blocking; run `impartus serve` without --json to start the API server",
		}))
	default:
		return newJSONError(command, fmt.Errorf("unknown command: %s", command))
	}
}

func runCourses(args []string) error {
	courses, err := getCourses(args)
	if err != nil {
		return err
	}
	return printJSON(courses)
}

func getCourses(args []string) (client.Courses, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("courses does not accept positional arguments")
	}

	ctx := context.Background()
	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return nil, err
	}

	courses, err := apiClient.GetCourses(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return courses, nil
}

func runLectures(args []string) error {
	lectures, err := getLectures(args)
	if err != nil {
		return err
	}
	return printJSON(lectures)
}

func getLectures(args []string) (client.Lectures, error) {
	fs := flag.NewFlagSet("lectures", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	subject := fs.Int("subject", 0, "Subject ID")
	fs.IntVar(subject, "s", 0, "Subject ID")
	session := fs.Int("session", 0, "Session ID")
	fs.IntVar(session, "S", 0, "Session ID")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, errors.New("lectures does not accept positional arguments")
	}
	if *subject <= 0 || *session <= 0 {
		return nil, errors.New("lectures requires --subject/-s and --session/-S")
	}

	ctx := context.Background()
	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return nil, err
	}

	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: *subject, SessionID: *session})
	if err != nil {
		return nil, err
	}

	return lectures, nil
}

func initClient(ctx context.Context) (*config.Config, *client.Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}

	apiClient, err := newLoggedInFn(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	return cfg, apiClient, nil
}

func loadConfig() (*config.Config, error) {
	cfg, err := loadResolvedFn("")
	if err != nil {
		return nil, err
	}
	cfg.Views = config.NormalizeViews(cfg.Views)
	return cfg, nil
}
