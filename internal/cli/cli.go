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
	runTUIFn                = runTUI
	isInteractiveTerminalFn = isInteractiveTerminal
	runCoursesFn            = runCourses
	runLecturesFn           = runLectures
	runDownloadFn           = runDownload
	runDownloadJSONFn       = runDownloadJSON
	runServeFn              = runServe
	runPlayFn               = runPlay
	runDoctorFn             = runDoctor
	runLibraryFn            = runLibrary
	runWatchFn              = runWatch
	runWatchJSONFn          = runWatchJSON
	loadResolvedFn          = config.LoadResolved
	newLoggedInFn           = client.NewLoggedIn
)

// Execute runs the root CLI command with the given version and build date.
func Execute(version, date string) error {
	args, jsonMode := stripGlobalJSONFlag(os.Args[1:])
	if len(args) == 0 {
		return executeDefault(jsonMode, version, date)
	}
	if help, ok := resolveCommandHelp(args); ok {
		if jsonMode {
			return emitJSONEnvelope(newSuccessEnvelope("help", newCommandHelpPayload(help)))
		}
		return showCommandHelp(os.Stdout, version, date, help)
	}
	if jsonMode {
		return executeJSON(args, version, date)
	}
	return executeHuman(args, version, date)
}

func executeDefault(jsonMode bool, version, date string) error {
	if jsonMode {
		return emitJSONEnvelope(newSuccessEnvelope("help", helpPayload()))
	}
	if isInteractiveTerminalFn() {
		return runTUIFn()
	}
	if err := showHelpTo(os.Stderr, version, date); err != nil {
		return err
	}
	return &exitCodeError{code: 2, err: errors.New("interactive TUI requires a terminal; choose an explicit command")}
}

func executeHuman(args []string, version, date string) error {
	switch args[0] {
	case "version", "--version", "-version", "-v":
		return executeHumanVersion(args, version, date)
	case "help", "--help", "-help", "-h":
		return showHelp(version, date)
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
	case "doctor":
		return runDoctorFn(args[1:])
	case "library":
		return runLibraryFn(args[1:])
	case "watch":
		return runWatchFn(args[1:])
	case "tui":
		return executeHumanTUI(args)
	default:
		if err := showHelp(version, date); err != nil {
			return err
		}
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func executeHumanVersion(args []string, version, date string) error {
	if len(args) > 1 {
		return fmt.Errorf("version does not accept positional arguments")
	}
	return showVersion(version, date)
}

func executeHumanTUI(args []string) error {
	if len(args) != 1 {
		return errors.New("tui does not accept positional arguments")
	}
	if !isInteractiveTerminalFn() {
		return &exitCodeError{code: 2, err: errors.New("interactive TUI requires a terminal")}
	}
	return runTUIFn()
}

func executeJSON(args []string, version, date string) error {
	command := args[0]
	switch command {
	case "version", "--version", "-version", "-v":
		return executeJSONVersion(args, version, date)
	case "help", "--help", "-help", "-h":
		return emitJSONEnvelope(newSuccessEnvelope("help", helpPayload()))
	case "courses":
		return executeJSONCourses(args[1:])
	case "lectures":
		return executeJSONLectures(args[1:])
	case "download":
		return executeJSONDownload(args[1:])
	case "play":
		return newJSONError("play", fmt.Errorf("play command is not supported in JSON mode"))
	case "doctor":
		return executeJSONDoctor(args[1:])
	case "library":
		return executeJSONLibrary(args[1:])
	case "watch":
		return executeJSONWatch(args[1:])
	case "tui":
		return newJSONError(command, fmt.Errorf("%s command is not supported in JSON mode", command))
	case "serve":
		return executeJSONServe(args[1:])
	default:
		return newJSONError(command, fmt.Errorf("unknown command: %s", command))
	}
}

func executeJSONVersion(args []string, version, date string) error {
	if len(args) > 1 {
		return newJSONError("version", fmt.Errorf("version does not accept positional arguments"))
	}
	return emitJSONEnvelope(newSuccessEnvelope("version", versionPayload{
		Name:      "impartus",
		Version:   version,
		BuildDate: date,
	}))
}

func executeJSONCourses(args []string) error {
	courses, err := getCourses(args)
	if err != nil {
		return newJSONError("courses", err)
	}
	return emitJSONEnvelope(newSuccessEnvelope("courses", courses))
}

func executeJSONLectures(args []string) error {
	lectures, err := getLectures(args)
	if err != nil {
		return newJSONError("lectures", err)
	}
	return emitJSONEnvelope(newSuccessEnvelope("lectures", lectures))
}

func executeJSONDownload(args []string) error {
	result, err := runDownloadJSONFn(args)
	if err != nil {
		jsonErr := newJSONError("download", err)
		if code := ExitCode(err); code != 1 {
			return &exitCodeError{code: code, err: jsonErr}
		}
		return jsonErr
	}
	return emitJSONEnvelope(newSuccessEnvelopeWithWarnings("download", result, result.Warnings))
}

func executeJSONWatch(args []string) error {
	result, err := runWatchJSONFn(args)
	if err != nil {
		return newJSONErrorWithData("watch", result, err)
	}
	return emitJSONEnvelope(newSuccessEnvelope("watch", result))
}

func executeJSONServe(args []string) error {
	port, err := parseServePort(args)
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
}

func executeJSONDoctor(args []string) error {
	report, err := getDoctorReport(args)
	if err != nil {
		return newJSONError("doctor", err)
	}
	if !report.OK {
		return newJSONErrorWithData("doctor", report, errors.New("doctor found one or more blocking problems"))
	}
	return emitJSONEnvelope(newSuccessEnvelope("doctor", report))
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
