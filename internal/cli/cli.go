package cli

import (
	"bufio"
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
		if err := validateDownloadArgs(args[1:]); err != nil {
			return newJSONError("download", err)
		}
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
	if err := validateDownloadArgs(args); err != nil {
		return downloadResult{}, err
	}
	return executeDownload(args)
}

func validateDownloadArgs(args []string) error {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	subject := fs.Int("subject", 0, "Subject ID")
	fs.IntVar(subject, "s", 0, "Subject ID")
	session := fs.Int("session", 0, "Session ID")
	fs.IntVar(session, "S", 0, "Session ID")
	start := fs.Int("start", 0, "Start lecture index (1-based)")
	end := fs.Int("end", 0, "End lecture index (1-based)")
	quality := fs.String("quality", "", "Video quality override")
	views := fs.String("views", "", "Views override")
	audioOnly := fs.Bool("audio-only", false, "Enable audio-only mode")
	format := fs.String("format", "", "Audio format override")
	output := fs.String("output", "", "Output directory override")
	fs.StringVar(output, "o", "", "Output directory override")
	skipNoAudio := fs.Bool("skip-no-audio", false, "Skip lectures with no audio track")
	includeNoAudio := fs.Bool("include-noaudio", false, "Include lectures with no audio track")
	_ = start
	_ = end
	_ = quality
	_ = views
	_ = audioOnly
	_ = format
	_ = output
	_ = skipNoAudio
	_ = includeNoAudio

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("download does not accept positional arguments")
	}
	if *subject <= 0 || *session <= 0 {
		return errors.New("download requires --subject/-s and --session/-S")
	}
	return nil
}

func executeDownload(args []string) (downloadResult, error) {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	subject := fs.Int("subject", 0, "Subject ID")
	fs.IntVar(subject, "s", 0, "Subject ID")
	session := fs.Int("session", 0, "Session ID")
	fs.IntVar(session, "S", 0, "Session ID")
	start := fs.Int("start", 0, "Start lecture index (1-based)")
	end := fs.Int("end", 0, "End lecture index (1-based)")
	quality := fs.String("quality", "", "Video quality override")
	views := fs.String("views", "", "Views override: left/right/both or first/second/both")
	audioOnly := fs.Bool("audio-only", false, "Enable audio-only mode")
	format := fs.String("format", "", "Audio format override")
	output := fs.String("output", "", "Output directory override")
	fs.StringVar(output, "o", "", "Output directory override")
	skipNoAudio := fs.Bool("skip-no-audio", false, "Skip lectures with no audio track")
	includeNoAudio := fs.Bool("include-noaudio", false, "Include lectures with no audio track (overrides --skip-no-audio)")

	if err := fs.Parse(args); err != nil {
		return downloadResult{}, err
	}
	if fs.NArg() > 0 {
		return downloadResult{}, errors.New("download does not accept positional arguments")
	}
	if *subject <= 0 || *session <= 0 {
		return downloadResult{}, errors.New("download requires --subject/-s and --session/-S")
	}

	if err := ensureFFmpeg(); err != nil {
		return downloadResult{}, err
	}

	ctx := context.Background()
	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return downloadResult{}, err
	}

	// Apply flag overrides and validate
	// Including noaudio is handled separately below as it overrides skip behavior
	cfg, err = applyAndValidateFlags(cfg, *quality, *views, *audioOnly, *format, *output, *skipNoAudio)
	if err != nil {
		return downloadResult{}, err
	}

	// Handle include-noaudio: it overrides skip-noaudio and config setting
	if *includeNoAudio {
		cfg.SkipNoAudio = false
	}

	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: *subject, SessionID: *session})
	if err != nil {
		return downloadResult{}, err
	}

	selected, err := selectLectureRange(lectures, *start, *end)
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
		cfg.Views = normalizeViews(views)
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

func runInteractive() error {
	if err := ensureFFmpeg(); err != nil {
		return err
	}

	ctx := context.Background()
	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Impartus Video Downloader")
	fmt.Println("If you are facing any issues, please check the section at https://github.com/rabesss/impartus-cli#faqtroubleshooting")
	fmt.Println()

	course, err := selectCourseInteractive(ctx, cfg, apiClient)
	if err != nil {
		return err
	}

	selected, err := filterLecturesInteractive(ctx, cfg, apiClient, course)
	if err != nil {
		return err
	}

	_, err = downloadLectures(ctx, cfg, apiClient, selected)
	return err
}

func selectCourseInteractive(ctx context.Context, cfg *config.Config, apiClient *client.Client) (*client.Course, error) {
	courses, err := apiClient.GetCourses(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if len(courses) == 0 {
		return nil, errors.New("no courses available")
	}

	for i, course := range courses {
		fmt.Printf("%3d %s\n", i+1, course.SubjectName)
	}

	reader := bufio.NewReader(os.Stdin)
	courseIndex, err := promptInt(reader, "Enter course number: ", 1, len(courses))
	if err != nil {
		return nil, err
	}

	return &courses[courseIndex-1], nil
}

func filterLecturesInteractive(ctx context.Context, cfg *config.Config, apiClient *client.Client, course *client.Course) (client.Lectures, error) {
	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: course.SubjectID, SessionID: course.SessionID})
	if err != nil {
		return nil, err
	}

	reversed := reverseLectures(lectures)
	for i, lecture := range reversed {
		fmt.Printf("%3d) LEC %3d %s\n", i+1, lecture.SeqNo, lecture.Topic)
	}

	reader := bufio.NewReader(os.Stdin)
	start, err := promptInt(reader, "Enter start lecture index: ", 1, len(reversed))
	if err != nil {
		return nil, err
	}
	end, err := promptInt(reader, "Enter end lecture index: ", start, len(reversed))
	if err != nil {
		return nil, err
	}

	skipEmpty, err := promptYesNo(reader, "Skip lectures with titles like 'No class' or 'No lecture'? [Y/n]: ", true)
	if err != nil {
		return nil, err
	}

	skipNoAudio, err := promptYesNo(reader, "Skip lectures without audio track? [Y/n]: ", true)
	if err != nil {
		return nil, err
	}

	selected := append(client.Lectures(nil), reversed[start-1:end]...)

	selected, emptyFiltered, noaudioFiltered := applyLectureFilters(selected, skipEmpty, skipNoAudio)

	if len(selected) == 0 {
		return nil, buildNoLecturesError(emptyFiltered, noaudioFiltered)
	}

	return selected, nil
}

func applyLectureFilters(lectures client.Lectures, skipEmpty, skipNoAudio bool) (client.Lectures, int, int) {
	emptyFiltered := 0
	noaudioFiltered := 0

	if skipEmpty {
		before := len(lectures)
		lectures = filterEmptyLectures(lectures)
		emptyFiltered = before - len(lectures)
	}
	if skipNoAudio {
		before := len(lectures)
		lectures = lectures.FilterNoAudio()
		noaudioFiltered = before - len(lectures)
	}

	return lectures, emptyFiltered, noaudioFiltered
}

func buildNoLecturesError(emptyFiltered, noaudioFiltered int) error {
	var reasons []string
	if emptyFiltered > 0 {
		reasons = append(reasons, fmt.Sprintf("%d empty", emptyFiltered))
	}
	if noaudioFiltered > 0 {
		reasons = append(reasons, fmt.Sprintf("%d noaudio", noaudioFiltered))
	}
	return fmt.Errorf("no lectures remaining after filtering: %s filtered out", strings.Join(reasons, ", "))
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
	if cfg.DownloadLocation == "" {
		cfg.DownloadLocation = "./downloads"
	}
	cfg.Views = normalizeViews(cfg.Views)
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

func reverseLectures(lectures client.Lectures) client.Lectures {
	return lectures.Reverse()
}

func selectLectureRange(lectures client.Lectures, start, end int) (client.Lectures, error) {
	reversed := reverseLectures(lectures)
	if len(reversed) == 0 {
		return nil, errors.New("no lectures found")
	}

	if start <= 0 {
		start = 1
	}
	if end <= 0 {
		end = len(reversed)
	}
	if start < 1 || end > len(reversed) || start > end {
		return nil, fmt.Errorf("invalid lecture range: start=%d end=%d (available 1-%d)", start, end, len(reversed))
	}

	return append(client.Lectures(nil), reversed[start-1:end]...), nil
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
		if lecture.Noaudio == 1 {
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

func normalizeViews(views string) string {
	switch strings.ToLower(strings.TrimSpace(views)) {
	case "first":
		return "left"
	case "second":
		return "right"
	default:
		return strings.ToLower(strings.TrimSpace(views))
	}
}

// validateFlagOverrides validates config values after CLI flag overrides are applied.
// This ensures invalid flag values fail early, before any remote API calls.
func validateFlagOverrides(cfg *config.Config) error {
	// Validate quality - must be one of the allowed values
	if cfg.Quality != "" && !map[string]bool{"144": true, "450": true, "720": true}[cfg.Quality] {
		return fmt.Errorf("invalid quality value %q: must be one of: 144, 450, 720", cfg.Quality)
	}

	// Validate views - must be one of the allowed values (both canonical and legacy)
	if cfg.Views != "" && !map[string]bool{"first": true, "second": true, "both": true, "left": true, "right": true}[cfg.Views] {
		return fmt.Errorf("invalid views value %q: must be one of: first, second, both, left, right", cfg.Views)
	}

	// Validate audio format when audio-only mode is enabled
	if cfg.AudioOnly && cfg.AudioFormat != "" && !map[string]bool{"mp3": true, "m4a": true, "aac": true, "opus": true}[cfg.AudioFormat] {
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

func promptInt(reader *bufio.Reader, prompt string, min, max int) (int, error) {
	for {
		fmt.Print(prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				line = strings.TrimSpace(line)
			} else {
				return 0, err
			}
		}

		value, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr != nil || value < min || value > max {
			fmt.Printf("Enter a number between %d and %d\n", min, max)
			if errors.Is(err, io.EOF) {
				return 0, errors.New("invalid input")
			}
			continue
		}

		return value, nil
	}
}

func promptYesNo(reader *bufio.Reader, prompt string, defaultYes bool) (bool, error) {
	for {
		fmt.Print(prompt)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}

		value := strings.ToLower(strings.TrimSpace(line))
		switch value {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			if errors.Is(err, io.EOF) {
				if defaultYes {
					return true, nil
				}
				return false, nil
			}
			fmt.Println("Enter y or n")
		}
	}
}

func ensureFFmpeg() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return errors.New("please add ffmpeg to your path")
	}
	return nil
}
