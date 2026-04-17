package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/vbauerster/mpb/v8"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/server"
)

var (
	runInteractiveFn = runInteractive
	runCoursesFn     = runCourses
	runLecturesFn    = runLectures
	runDownloadFn    = runDownload
	runServeFn       = runServe
)

type jsonEnvelope struct {
	Success bool     `json:"success"`
	Data    any      `json:"data"`
	Error   *jsonErr `json:"error"`
	Meta    jsonMeta `json:"meta"`
}

type jsonErr struct {
	Message string `json:"message"`
}

type jsonMeta struct {
	Command string `json:"command"`
	Mode    string `json:"mode"`
}

type jsonEnvelopeError struct {
	payload string
}

func (e jsonEnvelopeError) Error() string {
	return e.payload
}

type capabilityPayload struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	DefaultMode string              `json:"defaultMode"`
	Flags       []string            `json:"flags"`
	Commands    []capabilityCommand `json:"commands"`
}

type capabilityCommand struct {
	Name  string `json:"name"`
	Usage string `json:"usage"`
}

type versionPayload struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	BuildDate string `json:"buildDate"`
}

type downloadResult struct {
	Status        string   `json:"status"`
	OutputPaths   []string `json:"outputPaths"`
	LectureCount  int      `json:"lectureCount"`
	FilteredCount int      `json:"filteredCount,omitempty"`
	TotalLectures int      `json:"totalLectures,omitempty"`
}

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
		result, err := runDownloadJSON(args[1:])
		if err != nil {
			return newJSONError("download", err)
		}
		return emitJSONEnvelope(newSuccessEnvelope("download", result))
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

func stripGlobalJSONFlag(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	jsonMode := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, jsonMode
}

func newSuccessEnvelope(command string, data any) jsonEnvelope {
	return jsonEnvelope{
		Success: true,
		Data:    data,
		Error:   nil,
		Meta: jsonMeta{
			Command: command,
			Mode:    "json",
		},
	}
}

func newErrorEnvelope(command string, err error) jsonEnvelope {
	return jsonEnvelope{
		Success: false,
		Data:    nil,
		Error:   &jsonErr{Message: err.Error()},
		Meta: jsonMeta{
			Command: command,
			Mode:    "json",
		},
	}
}

func emitJSONEnvelope(payload jsonEnvelope) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(payload)
}

func newJSONError(command string, err error) error {
	payload, marshalErr := json.Marshal(newErrorEnvelope(command, err))
	if marshalErr != nil {
		return err
	}
	return jsonEnvelopeError{payload: string(payload)}
}

func helpPayload() capabilityPayload {
	return capabilityPayload{
		Name:        "impartus",
		Description: "CLI and interactive downloader for Impartus lectures",
		DefaultMode: "interactive",
		Flags:       []string{"--json"},
		Commands: []capabilityCommand{
			{Name: "help", Usage: "impartus help"},
			{Name: "version", Usage: "impartus version"},
			{Name: "courses", Usage: "impartus courses"},
			{Name: "lectures", Usage: "impartus lectures --subject <id> --session <id>"},
			{Name: "download", Usage: "impartus download --subject <id> --session <id> [--start <n>] [--end <n>]"},
			{Name: "serve", Usage: "impartus serve [--port <port>]"},
		},
	}
}

func parseServePort(args []string) (int, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 8080, "Port")
	if err := fs.Parse(args); err != nil {
		return 0, err
	}
	if fs.NArg() > 0 {
		return 0, fmt.Errorf("serve does not accept positional arguments")
	}
	return *port, nil
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

func runDownload(args []string) error {
	_, err := executeDownload(args)
	return err
}

func runDownloadJSON(args []string) (downloadResult, error) {
	return executeDownload(args)
}

// downloadFlags holds parsed download command flags.
type downloadFlags struct {
	subject        int
	session        int
	start          int
	end            int
	quality        string
	views          string
	audioOnly      bool
	format         string
	output         string
	skipNoAudio    bool
	includeNoAudio bool
}

func parseDownloadFlags(args []string) (downloadFlags, error) {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f downloadFlags
	fs.IntVar(&f.subject, "subject", 0, "Subject ID")
	fs.IntVar(&f.subject, "s", 0, "Subject ID")
	fs.IntVar(&f.session, "session", 0, "Session ID")
	fs.IntVar(&f.session, "S", 0, "Session ID")
	fs.IntVar(&f.start, "start", 0, "Start lecture index (1-based)")
	fs.IntVar(&f.end, "end", 0, "End lecture index (1-based)")
	fs.StringVar(&f.quality, "quality", "", "Video quality override")
	fs.StringVar(&f.views, "views", "", "Views override: left/right/both or first/second/both")
	fs.BoolVar(&f.audioOnly, "audio-only", false, "Enable audio-only mode")
	fs.StringVar(&f.format, "format", "", "Audio format override")
	fs.StringVar(&f.output, "output", "", "Output directory override")
	fs.StringVar(&f.output, "o", "", "Output directory override")
	fs.BoolVar(&f.skipNoAudio, "skip-no-audio", false, "Skip lectures with no audio track")
	fs.BoolVar(&f.includeNoAudio, "include-noaudio", false, "Include lectures with no audio track (overrides --skip-no-audio)")

	if err := fs.Parse(args); err != nil {
		return downloadFlags{}, err
	}
	if fs.NArg() > 0 {
		return downloadFlags{}, errors.New("download does not accept positional arguments")
	}
	if f.subject <= 0 || f.session <= 0 {
		return downloadFlags{}, errors.New("download requires --subject/-s and --session/-S")
	}
	return f, nil
}

func executeDownload(args []string) (downloadResult, error) {
	f, err := parseDownloadFlags(args)
	if err != nil {
		return downloadResult{}, err
	}

	if err := ensureFFmpeg(); err != nil {
		return downloadResult{}, err
	}

	ctx := context.Background()
	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return downloadResult{}, err
	}

	cfg, err = applyAndValidateFlags(cfg, f.quality, f.views, f.audioOnly, f.format, f.output, f.skipNoAudio)
	if err != nil {
		return downloadResult{}, err
	}

	if f.includeNoAudio {
		cfg.SkipNoAudio = false
	}

	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: f.subject, SessionID: f.session})
	if err != nil {
		return downloadResult{}, err
	}

	selected, err := lectures.SelectRange(f.start, f.end)
	if err != nil {
		return downloadResult{}, err
	}

	// Count noaudio lectures for warning
	warnNoAudioLectures(selected, cfg.SkipNoAudio)

	// Apply noaudio filter if flag is set
	totalLectures := len(selected)
	if cfg.SkipNoAudio {
		selected = selected.FilterNoAudio()
	}

	if len(selected) == 0 {
		return downloadResult{}, fmt.Errorf("no lectures available after filtering (all lectures have noaudio=1 in the selected range)")
	}

	result, err := downloadLectures(ctx, cfg, apiClient, selected)
	if err != nil {
		return downloadResult{}, err
	}
	result.FilteredCount = totalLectures - len(selected)
	result.TotalLectures = totalLectures
	return result, nil
}

// applyAndValidateFlags applies CLI flag overrides to the config and validates them.
// This ensures invalid flag values fail early, before any remote API calls.
func applyAndValidateFlags(cfg *config.Config, quality, views string, audioOnly bool, format, output string, skipNoAudio bool) (*config.Config, error) {
	// Apply flag overrides
	if quality != "" {
		cfg.Quality = quality
	}
	if views != "" {
		cfg.Views = config.NormalizeViews(views)
	}
	if audioOnly {
		cfg.AudioOnly = true
	}
	if format != "" {
		cfg.AudioFormat = format
	}
	if output != "" {
		cfg.DownloadLocation = output
	}
	if skipNoAudio {
		cfg.SkipNoAudio = true
	}

	// Validate flag override values
	if err := validateFlagOverrides(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func runServe(args []string, _ string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 8080, "Port")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("serve does not accept positional arguments")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	srv := server.NewAPIServerWithPersistence(strconv.Itoa(*port), cfg, "")
	return srv.Start(context.Background())
}

func initClient(ctx context.Context) (*config.Config, *client.Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}

	apiClient := client.New(nil, nil)
	if err := apiClient.LoginAndSetToken(ctx, cfg); err != nil {
		return nil, nil, err
	}

	return cfg, apiClient, nil
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.LoadResolved("")
	if err != nil {
		return nil, err
	}
	cfg.Views = config.NormalizeViews(cfg.Views)
	return cfg, nil
}

func downloadLectures(ctx context.Context, cfg *config.Config, apiClient *client.Client, lectures client.Lectures) (downloadResult, error) {
	if len(lectures) == 0 {
		return downloadResult{}, errors.New("no lectures selected")
	}

	if err := os.MkdirAll(cfg.DownloadLocation, 0o755); err != nil {
		return downloadResult{}, err
	}

	d := downloader.New(cfg, apiClient)
	playlists, err := d.FetchLecturePlaylists(ctx, lectures)
	if err != nil {
		return downloadResult{}, err
	}
	if len(playlists) == 0 {
		return downloadResult{}, errors.New("no playlists available for selected lectures")
	}

	p := mpb.New(mpb.WithWidth(70))
	var tracker *downloader.ProgressTracker
	if cfg.ProgressTracking.Enabled {
		tracker = downloader.NewProgressTracker(len(playlists), countChunks(playlists, cfg.Views), p)
	}

	outputPaths := make([]string, 0, len(playlists))
	for _, playlist := range playlists {
		downloaded, err := d.DownloadPlaylist(ctx, playlist, p, tracker)
		if err != nil {
			return downloadResult{}, err
		}

		metadataFile, err := d.CreateTempM3U8File(downloaded)
		if err != nil {
			return downloadResult{}, err
		}

		joinResult, err := d.JoinLectureOutput(metadataFile)
		if err != nil {
			return downloadResult{}, err
		}
		outputPaths = appendOutputPaths(outputPaths, joinResult)

		if tracker != nil {
			downloader.LectureCompleted(tracker)
		}
	}

	if tracker != nil {
		tracker.Stop()
	}

	p.Wait()
	return downloadResult{Status: "completed", OutputPaths: outputPaths, LectureCount: len(outputPaths)}, nil
}

func appendOutputPaths(outputPaths []string, result downloader.JoinResult) []string {
	for _, path := range []string{result.LeftOutput, result.RightOutput, result.BothOutput} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		outputPaths = append(outputPaths, path)
	}
	return outputPaths
}

func filterEmptyLectures(lectures client.Lectures) client.Lectures {
	filtered := make(client.Lectures, 0, len(lectures))
	for _, lecture := range lectures {
		topic := strings.ToLower(strings.TrimSpace(lecture.Topic))
		if strings.Contains(topic, "no class") || strings.Contains(topic, "no lecture") {
			continue
		}
		filtered = append(filtered, lecture)
	}
	return filtered
}

func countNoAudioLectures(lectures client.Lectures) int {
	count := 0
	for _, lecture := range lectures {
		if lecture.NoAudio == 1 {
			count++
		}
	}
	return count
}

func warnNoAudioLectures(lectures client.Lectures, skipNoAudio bool) {
	noaudioCount := countNoAudioLectures(lectures)
	if noaudioCount > 0 && !skipNoAudio {
		fmt.Printf("[WARNING] %d lecture(s) in selection have no audio track (noaudio=1)\n", noaudioCount)
		fmt.Printf("[INFO] Use --skip-no-audio to filter these out, or --include-noaudio to include anyway\n")
	}
}

func countChunks(playlists []client.ParsedPlaylist, views string) int {
	total := 0
	for _, playlist := range playlists {
		if views != "right" {
			total += len(playlist.FirstViewURLs)
		}
		if views != "left" {
			total += len(playlist.SecondViewURLs)
		}
	}
	return total
}

// validateFlagOverrides validates config values after CLI flag overrides are applied.
// This ensures invalid flag values fail early, before any remote API calls.
func validateFlagOverrides(cfg *config.Config) error {
	if cfg.Quality != "" && !config.OneOf(cfg.Quality, "144", "450", "720") {
		return fmt.Errorf("invalid quality value %q: must be one of: 144, 450, 720", cfg.Quality)
	}
	if cfg.Views != "" && !config.OneOf(cfg.Views, "first", "second", "both", "left", "right") {
		return fmt.Errorf("invalid views value %q: must be one of: first, second, both, left, right", cfg.Views)
	}
	if cfg.AudioOnly && cfg.AudioFormat != "" && !config.OneOf(cfg.AudioFormat, "mp3", "m4a", "aac", "opus") {
		return fmt.Errorf("invalid audioFormat value %q: must be one of: mp3, m4a, aac, opus", cfg.AudioFormat)
	}
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(v)
}

func showVersion(version, date string) {
	fmt.Printf("Impartus Video Downloader\nVersion: %s\nBuild Date: %s\n", version, date)
}

func showHelp(version, date string) {
	showVersion(version, date)
	fmt.Println("\nUsage:")
	fmt.Println("  impartus [command] [flags]")
	fmt.Println("\nCommands:")
	fmt.Println("  courses                              List courses (JSON)")
	fmt.Println("  lectures -s <subject> -S <session>   List lectures (JSON)")
	fmt.Println("  download [flags]                     Download lectures")
	fmt.Println("  serve [--port <port>]                Start HTTP API server")
	fmt.Println("  version                              Show version")
	fmt.Println("  help                                 Show help")
	fmt.Println("\nDownload Flags:")
	fmt.Println("  --subject,-s        Subject ID")
	fmt.Println("  --session,-S        Session ID")
	fmt.Println("  --start             Start lecture index (1-based)")
	fmt.Println("  --end               End lecture index (1-based)")
	fmt.Println("  --quality           Quality override")
	fmt.Println("  --views             Views override")
	fmt.Println("  --audio-only        Audio-only mode")
	fmt.Println("  --format            Audio format override")
	fmt.Println("  --output,-o         Output directory")
	fmt.Println("  --skip-no-audio     Skip lectures with no audio track")
	fmt.Println("  --include-noaudio   Include noaudio lectures (overrides --skip-no-audio)")
	fmt.Println("\nNo command starts interactive download mode.")
}

func ensureFFmpeg() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errors.New("please add ffmpeg to your path")
	}
	return nil
}
