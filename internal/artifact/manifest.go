package artifact

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SchemaVersionV1 is the first stable download-manifest schema.
const SchemaVersionV1 = 1

// Manifest describes one completed logical lecture download and all files it
// materialized locally.
type Manifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	ArtifactID    string    `json:"artifactId"`
	Lecture       Lecture   `json:"lecture"`
	Selection     Selection `json:"selection"`
	Files         []File    `json:"files"`
	ProducedAt    time.Time `json:"producedAt"`
	Producer      Producer  `json:"producer"`
}

// Lecture is the stable subset of upstream lecture metadata exposed to local
// consumers. The four IDs scope the artifact independently of display text.
type Lecture struct {
	TTID            int    `json:"ttid"`
	InstituteID     int    `json:"instituteId"`
	SubjectID       int    `json:"subjectId"`
	SessionID       int    `json:"sessionId"`
	SeqNo           int    `json:"seqNo"`
	Topic           string `json:"topic"`
	StartTime       string `json:"startTime"`
	DurationSeconds int    `json:"durationSeconds"`
	Professor       string `json:"professor"`
	Institute       string `json:"institute"`
	NoAudio         bool   `json:"noAudio"`
}

// Selection records the media choices that are part of logical identity.
type Selection struct {
	Views       string `json:"views"`
	Quality     string `json:"quality"`
	AudioOnly   bool   `json:"audioOnly"`
	AudioFormat string `json:"audioFormat"`
}

// File is a verified, materialized output file.
type File struct {
	Path      string `json:"path"`
	Role      string `json:"role"`
	View      string `json:"view"`
	Container string `json:"container"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256,omitempty"`
}

// FileSpec assigns typed output metadata before Build verifies the file on
// disk. Role and view should come from the downloader's typed join result.
type FileSpec struct {
	Path      string
	Role      string
	View      string
	Container string
	SHA256    string
}

// Producer identifies the program version that materialized the artifact.
type Producer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// BuildInput contains all values needed to verify and construct a manifest.
type BuildInput struct {
	Lecture    Lecture
	Selection  Selection
	Files      []FileSpec
	ProducedAt time.Time
	Producer   Producer
}

// Build validates a completed output set and returns its versioned manifest.
// It never emits a manifest for missing, partial, empty, or non-regular files.
func Build(input BuildInput) (Manifest, error) {
	identity := Identity{
		InstituteID: input.Lecture.InstituteID,
		SubjectID:   input.Lecture.SubjectID,
		SessionID:   input.Lecture.SessionID,
		TTID:        input.Lecture.TTID,
		AudioOnly:   input.Selection.AudioOnly,
		Views:       input.Selection.Views,
		Quality:     input.Selection.Quality,
		AudioFormat: input.Selection.AudioFormat,
	}
	normalized, err := normalizeIdentity(identity)
	if err != nil {
		return Manifest{}, err
	}
	if input.ProducedAt.IsZero() {
		return Manifest{}, errors.New("producedAt is required")
	}
	input.Producer.Name = strings.TrimSpace(input.Producer.Name)
	input.Producer.Version = strings.TrimSpace(input.Producer.Version)
	if input.Producer.Name == "" || input.Producer.Version == "" {
		return Manifest{}, errors.New("producer name and version are required")
	}
	if len(input.Files) == 0 {
		return Manifest{}, errors.New("at least one completed output file is required")
	}

	files := make([]File, 0, len(input.Files))
	seenPaths := make(map[string]struct{}, len(input.Files))
	for _, spec := range input.Files {
		file, fileErr := verifyFile(spec, normalized.AudioOnly, normalized.Views)
		if fileErr != nil {
			return Manifest{}, fileErr
		}
		if _, exists := seenPaths[file.Path]; exists {
			return Manifest{}, fmt.Errorf("duplicate output path %q", file.Path)
		}
		seenPaths[file.Path] = struct{}{}
		files = append(files, file)
	}

	id, err := NewID(normalized)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		SchemaVersion: SchemaVersionV1,
		ArtifactID:    id,
		Lecture:       input.Lecture,
		Selection: Selection{
			Views:       normalized.Views,
			Quality:     normalized.Quality,
			AudioOnly:   normalized.AudioOnly,
			AudioFormat: normalized.AudioFormat,
		},
		Files:      files,
		ProducedAt: input.ProducedAt.UTC(),
		Producer:   input.Producer,
	}, nil
}

// Identity reconstructs the exact logical identity stored in a manifest.
func (manifest Manifest) Identity() Identity {
	return Identity{
		InstituteID: manifest.Lecture.InstituteID,
		SubjectID:   manifest.Lecture.SubjectID,
		SessionID:   manifest.Lecture.SessionID,
		TTID:        manifest.Lecture.TTID,
		AudioOnly:   manifest.Selection.AudioOnly,
		Views:       manifest.Selection.Views,
		Quality:     manifest.Selection.Quality,
		AudioFormat: manifest.Selection.AudioFormat,
	}
}

func verifyFile(spec FileSpec, audioOnly bool, selectedViews string) (File, error) {
	absolutePath, file, info, err := openCompletedFile(spec.Path)
	if err != nil {
		return File{}, err
	}
	defer func() {
		closeErr := file.Close()
		_ = closeErr
	}()
	role, view, container, sha256Hex, err := normalizeFileSpec(spec, audioOnly, selectedViews)
	if err != nil {
		return File{}, err
	}
	size := info.Size()
	if sha256Hex != "" {
		size, err = verifySHA256(file, absolutePath, sha256Hex)
		if err != nil {
			return File{}, err
		}
	}
	return File{
		Path:      absolutePath,
		Role:      role,
		View:      view,
		Container: container,
		Bytes:     size,
		SHA256:    sha256Hex,
	}, nil
}

func openCompletedFile(rawPath string) (string, *os.File, os.FileInfo, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", nil, nil, errors.New("output path is required")
	}
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", nil, nil, fmt.Errorf("normalize output path: %w", err)
	}
	if strings.HasSuffix(strings.ToLower(absolutePath), ".part") {
		return "", nil, nil, fmt.Errorf("output %q is still partial", absolutePath)
	}
	file, err := os.Open(absolutePath) // #nosec G304 -- path was explicitly supplied by the caller
	if err != nil {
		return "", nil, nil, fmt.Errorf("stat output %q: %w", absolutePath, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close() //nolint:errcheck // preserve the primary stat failure
		return "", nil, nil, fmt.Errorf("stat output %q: %w", absolutePath, err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close() //nolint:errcheck // validation failure is the actionable error
		return "", nil, nil, fmt.Errorf("output %q is not a regular file", absolutePath)
	}
	if info.Size() <= 0 {
		_ = file.Close() //nolint:errcheck // validation failure is the actionable error
		return "", nil, nil, fmt.Errorf("output %q is empty", absolutePath)
	}
	return absolutePath, file, info, nil
}

func normalizeFileSpec(spec FileSpec, audioOnly bool, selectedViews string) (string, string, string, string, error) {
	role := strings.ToLower(strings.TrimSpace(spec.Role))
	wantRole := "video"
	if audioOnly {
		wantRole = "audio"
	}
	if role != wantRole {
		return "", "", "", "", fmt.Errorf("output role %q does not match selection role %q", role, wantRole)
	}
	view := strings.ToLower(strings.TrimSpace(spec.View))
	switch view {
	case "left", "right", "both":
	default:
		return "", "", "", "", fmt.Errorf("unsupported output view %q", view)
	}
	if selectedViews != "both" && view != selectedViews {
		return "", "", "", "", fmt.Errorf("output view %q is outside selected views %q", view, selectedViews)
	}
	container := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(spec.Container)), ".")
	if err := validateContainer(container, audioOnly); err != nil {
		return "", "", "", "", err
	}

	sha256Hex := strings.ToLower(strings.TrimSpace(spec.SHA256))
	if sha256Hex != "" && !validSHA256Hex(sha256Hex) {
		return "", "", "", "", errors.New("sha256 must be 64 lowercase hexadecimal characters")
	}
	return role, view, container, sha256Hex, nil
}

func verifySHA256(file *os.File, path, expected string) (int64, error) {
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return 0, fmt.Errorf("hash output %q: %w", path, err)
	}
	if size <= 0 {
		return 0, fmt.Errorf("output %q is empty", path)
	}
	actual := fmt.Sprintf("%x", hasher.Sum(nil))
	if actual != expected {
		return 0, fmt.Errorf("sha256 for output %q does not match file contents", path)
	}
	return size, nil
}

func validateContainer(container string, audioOnly bool) error {
	if audioOnly {
		switch container {
		case "mp3", "m4a", "aac", "opus":
			return nil
		}
		return fmt.Errorf("unsupported audio container %q", container)
	}
	switch container {
	case "mp4", "mkv":
		return nil
	default:
		return fmt.Errorf("unsupported video container %q", container)
	}
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
