package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// These tests pin the container build contract from issue #304: one Go
// version everywhere, a cross-compiled build stage, a Dependabot-trackable
// runtime base and an ffmpeg smoke test of the built image.

func readRepoFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Line-anchored checks must also hold for core.autocrlf=true checkouts.
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

var (
	goDirectivePattern        = regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)\s*$`)
	toolchainDirectivePattern = regexp.MustCompile(`(?m)^toolchain go(\d+\.\d+\.\d+)\s*$`)
	// Any Debian codename is accepted, so a move to a new Debian release
	// changes the Dockerfile and not these tests. Both stages must use the same one.
	buildStagePattern   = regexp.MustCompile(`(?m)^FROM (--platform=\$BUILDPLATFORM )?golang:(\d+\.\d+\.\d+)-([a-z]+)@sha256:[0-9a-f]{64} AS build$`)
	runtimeStagePattern = regexp.MustCompile(`(?m)^FROM (debian:\S+)$`)
	datedDebianPattern  = regexp.MustCompile(`^debian:([a-z]+)-\d{8}-slim@sha256:[0-9a-f]{64}$`)
	// ffprobe piped into another command hides its exit status in dash, which has no pipefail.
	ffprobePipePattern = regexp.MustCompile(`(?m)^[^#\n]*\bffprobe\b[^\n]*\|`)
)

// goModVersion returns the single Go version CI, release and the Dockerfile
// build with: the go.mod toolchain directive if present, else the go directive
// (setup-go's go-version-file resolves it the same way).
func goModVersion(t *testing.T) string {
	t.Helper()

	goMod := readRepoFile(t, "go.mod")
	toolchains := toolchainDirectivePattern.FindAllStringSubmatch(goMod, -1)
	switch len(toolchains) {
	case 0:
	case 1:
		return toolchains[0][1]
	default:
		t.Fatalf("go.mod has %d toolchain directives, want at most one", len(toolchains))
	}

	match := goDirectivePattern.FindStringSubmatch(goMod)
	if match == nil {
		t.Fatal("go.mod is missing its go directive")
	}
	if strings.Count(match[1], ".") != 2 {
		t.Fatalf("go directive %q must name an exact patch release (goX.Y.Z) so every build uses the same Go", match[1])
	}
	return match[1]
}

func parseGoVersion(t *testing.T, version string) [3]int {
	t.Helper()

	var parsed [3]int
	for i, part := range strings.Split(version, ".") {
		if i >= len(parsed) {
			t.Fatalf("malformed Go version %q", version)
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				t.Fatalf("malformed Go version %q", version)
			}
			parsed[i] = parsed[i]*10 + int(r-'0')
		}
	}
	return parsed
}

func goVersionLessOrEqual(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return true
}

func TestGoModIsTheSingleGoVersion(t *testing.T) {
	version := goModVersion(t)

	match := goDirectivePattern.FindStringSubmatch(readRepoFile(t, "go.mod"))
	if match == nil {
		t.Fatal("go.mod is missing its go directive")
	}
	if !goVersionLessOrEqual(parseGoVersion(t, match[1]), parseGoVersion(t, version)) {
		t.Errorf("go directive %s is newer than toolchain go%s", match[1], version)
	}

	dockerfile := readRepoFile(t, "Dockerfile")
	build := buildStagePattern.FindStringSubmatch(dockerfile)
	if build == nil {
		t.Fatal("Dockerfile build stage must be `FROM --platform=$BUILDPLATFORM golang:<go.mod version>-<debian codename>@sha256:<digest> AS build`")
	}
	if build[2] != version {
		t.Errorf("Dockerfile builds with golang:%s, but go.mod selects go%s; bump both in one change", build[2], version)
	}
}

func TestWorkflowsTakeGoVersionFromGoMod(t *testing.T) {
	paths, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no workflows found")
	}

	setupGoUses := 0
	for _, path := range paths {
		findings, uses := goVersionPinFindings(readRepoFile(t, path))
		setupGoUses += uses
		for _, finding := range findings {
			t.Errorf("%s:%s", path, finding)
		}
	}
	if setupGoUses == 0 {
		t.Fatal("no actions/setup-go uses found")
	}
}

var goVersionFilePattern = regexp.MustCompile(`^\s*go-version-file: '?go\.mod'?\s*$`)

// goVersionPinFindings reports workflow lines that pin a Go version other than
// go.mod's, and counts actions/setup-go uses. Full-line YAML comments are
// skipped; a trailing comment after a real key is still checked.
func goVersionPinFindings(workflow string) (findings []string, setupGoUses int) {
	lines := strings.Split(workflow, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "go-version:") {
			findings = append(findings, fmt.Sprintf("%d pins a Go version; use go-version-file: go.mod", i+1))
		}
		if strings.Contains(line, "GOTOOLCHAIN") {
			findings = append(findings, fmt.Sprintf("%d overrides GOTOOLCHAIN; go.mod is the only Go version", i+1))
		}
		if !strings.Contains(line, "uses: actions/setup-go@") {
			continue
		}
		setupGoUses++
		found := false
		for _, next := range lines[i+1 : min(i+6, len(lines))] {
			if goVersionFilePattern.MatchString(next) {
				found = true
				break
			}
		}
		if !found {
			findings = append(findings, fmt.Sprintf("%d sets up Go without go-version-file: go.mod", i+1))
		}
	}
	return findings, setupGoUses
}

func TestGoVersionPinFindingsSkipsCommentLines(t *testing.T) {
	cases := []struct {
		name     string
		workflow string
		want     int
	}{
		{"comment mentioning go-version", "    # do not add go-version: here\n", 0},
		{"comment mentioning GOTOOLCHAIN", "  # GOTOOLCHAIN=local would override go.mod\n", 0},
		{"real go-version pin", "        with:\n          go-version: '1.27'\n", 1},
		{"GOTOOLCHAIN in run", "        run: GOTOOLCHAIN=local go test ./...\n", 1},
		{"trailing comment does not hide a pin", "          go-version: '1.27' # temporary\n", 1},
		{"setup-go without go-version-file", "      - uses: actions/setup-go@abc # v7\n        with:\n          cache: true\n", 1},
		{"setup-go with go-version-file", "      - uses: actions/setup-go@abc # v7\n        with:\n          go-version-file: 'go.mod'\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := goVersionPinFindings(tc.workflow); len(got) != tc.want {
				t.Errorf("goVersionPinFindings = %q, want %d finding(s)", got, tc.want)
			}
		})
	}
}

func TestCIRunsTheGoModVersion(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("only meaningful under actions/setup-go")
	}
	if want := "go" + goModVersion(t); runtime.Version() != want {
		t.Errorf("CI runs %s, want %s from go.mod", runtime.Version(), want)
	}
}

func TestDockerfileCrossCompilesBuildStage(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")

	build := buildStagePattern.FindStringSubmatch(dockerfile)
	if build == nil || build[1] == "" {
		t.Fatal("Dockerfile build stage must run on $BUILDPLATFORM so arm64 cross-compiles instead of building under QEMU")
	}

	download := strings.Index(dockerfile, "RUN go mod download")
	targetArgs := strings.Index(dockerfile, "ARG TARGETOS TARGETARCH")
	goBuild := strings.Index(dockerfile, "go build")
	if download == -1 || targetArgs == -1 || goBuild == -1 {
		t.Fatalf("Dockerfile is missing go mod download (%d), ARG TARGETOS TARGETARCH (%d) or go build (%d)", download, targetArgs, goBuild)
	}
	if download > targetArgs || targetArgs > goBuild {
		t.Error("declare ARG TARGETOS TARGETARCH after go mod download so the module layer is shared across platforms")
	}
	if !strings.Contains(dockerfile, "CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}") {
		t.Error("go build must cross-compile with CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}")
	}
	if strings.Contains(dockerfile, "TARGETARCH=amd64") || strings.Contains(dockerfile, "TARGETOS=linux") {
		t.Error("TARGETOS/TARGETARCH defaults would mask a missing BuildKit platform")
	}
}

func TestDockerfileRuntimeBaseIsDependabotTrackable(t *testing.T) {
	match := runtimeStagePattern.FindStringSubmatch(readRepoFile(t, "Dockerfile"))
	if match == nil {
		t.Fatal("Dockerfile is missing the debian runtime stage")
	}
	// A rolling tag such as bookworm-slim gives Dependabot a version that never
	// changes, so one closed digest PR (#75) suppressed every later refresh (#304).
	base := datedDebianPattern.FindStringSubmatch(match[1])
	if base == nil {
		t.Fatalf("runtime base %q must be a dated debian:<codename>-YYYYMMDD-slim@sha256 pin so Dependabot sees new versions", match[1])
	}
	build := buildStagePattern.FindStringSubmatch(readRepoFile(t, "Dockerfile"))
	if build == nil {
		t.Fatal("Dockerfile is missing the golang build stage")
	}
	if build[3] != base[1] {
		t.Errorf("build stage uses Debian %q but the runtime base uses %q; move both together", build[3], base[1])
	}
}

func TestDockerfileBasePatternsAcceptAnyDebianCodename(t *testing.T) {
	const digest = "@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	builds := []struct {
		line string
		want bool
	}{
		{"FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm" + digest + " AS build", true},
		{"FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie" + digest + " AS build", true},
		{"FROM --platform=$BUILDPLATFORM golang:1.27.3" + digest + " AS build", false},
		{"FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie AS build", false},
		{"FROM --platform=$BUILDPLATFORM golang:1.27-trixie" + digest + " AS build", false},
	}
	for _, tc := range builds {
		if got := buildStagePattern.MatchString(tc.line); got != tc.want {
			t.Errorf("buildStagePattern.MatchString(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
	runtimes := []struct {
		image string
		want  bool
	}{
		{"debian:bookworm-20261104-slim" + digest, true},
		{"debian:trixie-20261104-slim" + digest, true},
		{"debian:trixie-slim" + digest, false},
		{"debian:trixie-20261104-slim", false},
		{"debian:trixie-20261104" + digest, false},
	}
	for _, tc := range runtimes {
		if got := datedDebianPattern.MatchString(tc.image); got != tc.want {
			t.Errorf("datedDebianPattern.MatchString(%q) = %v, want %v", tc.image, got, tc.want)
		}
	}
}

func TestContainerSmokeScriptCoversCLIAndFFmpegPaths(t *testing.T) {
	const path = "scripts/container-smoke.sh"
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// Windows reports no Unix execute bits; git still records the mode there.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable", path)
	}

	script := readRepoFile(t, path)
	required := []string{
		"set -euo pipefail",
		`--platform "$platform"`,
		"--help",
		// Old release images run as a non-root user without a writable /work.
		"--tmpfs /smoke",
		"--workdir /smoke",
		// Encode an HLS source, then exercise each internal/downloader/ffmpeg.go path.
		"-f hls",
		"-c:v libx264",
		"-c:a aac",
		"-map 0 -map 1 -c copy",
		"-f matroska",
		"-acodec libmp3lame",
		"-f ipod",
		"-acodec libopus",
		"-f opus",
		"-xerror",
		"-f null",
		"ffprobe",
	}
	for _, snippet := range required {
		if !strings.Contains(script, snippet) {
			t.Errorf("%s is missing %q", path, snippet)
		}
	}
	if strings.Contains(script, "/work") {
		t.Errorf("%s must not depend on /work, which older release images do not make writable", path)
	}
	if !strings.Contains(script, "ffprobe failed") {
		t.Errorf("%s must report an ffprobe failure itself", path)
	}
	if pipe := ffprobePipePattern.FindString(script); pipe != "" {
		t.Errorf("%s pipes ffprobe into another command, which hides its exit status in dash: %q", path, pipe)
	}
}

func TestFFprobePipePattern(t *testing.T) {
	cases := []struct {
		script string
		want   bool
	}{
		{"video=$(ffprobe -v error views.mkv | wc -l)\n", true},
		{"if ! streams=$(ffprobe -v error views.mkv); then\n", false},
		{"# ffprobe output | wc -l used to hide failures\n", false},
		{"video=$(printf '%s\\n' \"$streams\" | grep -cx video || true)\n", false},
	}
	for _, tc := range cases {
		if got := ffprobePipePattern.MatchString(tc.script); got != tc.want {
			t.Errorf("ffprobePipePattern.MatchString(%q) = %v, want %v", tc.script, got, tc.want)
		}
	}
}

func TestCIContainerJobRunsSmokeScript(t *testing.T) {
	ci := readRepoFile(t, ".github/workflows/ci.yml")

	start := strings.Index(ci, "  container-permissions:")
	if start == -1 {
		t.Fatal("ci.yml is missing the container-permissions job")
	}
	job := ci[start:]
	if end := strings.Index(job[1:], "\n  build:"); end != -1 {
		job = job[:end+1]
	}

	required := []string{
		"docker build -t impartus:permission-test .",
		"test -w /work -a -w /work/temp -a -w /work/downloads",
		"docker run --rm impartus:permission-test --help",
		"scripts/container-smoke.sh impartus:permission-test linux/amd64",
	}
	for _, snippet := range required {
		if !strings.Contains(job, snippet) {
			t.Errorf("container-permissions job is missing %q", snippet)
		}
	}
}

func TestReadRepoFileNormalisesCRLF(t *testing.T) {
	const digest = "@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	raw := "FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm" + digest + " AS build\r\n" +
		"RUN go build\r\n" +
		"FROM debian:bookworm-20261104-slim" + digest + "\r\n"
	path := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	// core.autocrlf=true checkouts must not break the line-anchored contract checks.
	got := readRepoFile(t, path)
	if want := strings.ReplaceAll(raw, "\r\n", "\n"); got != want {
		t.Errorf("readRepoFile = %q, want %q", got, want)
	}
	// The raw bytes defeat the $-anchored pattern, so the normalisation is what makes the checks pass.
	if buildStagePattern.MatchString(raw) {
		t.Fatal("buildStagePattern matched CRLF input; this fixture no longer proves normalisation matters")
	}
	if build := buildStagePattern.FindStringSubmatch(got); build == nil || build[2] != "1.27.3" || build[3] != "bookworm" {
		t.Errorf("buildStagePattern on normalised Dockerfile = %q", build)
	}
	stage := runtimeStagePattern.FindStringSubmatch(got)
	if stage == nil || !datedDebianPattern.MatchString(stage[1]) {
		t.Errorf("runtime stage on normalised Dockerfile = %q, want a dated debian pin", stage)
	}
}
