package artifact

import (
	"crypto/sha256"
	"errors"
	"fmt"
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
		file, fileErr := verifyFile(spec, normalized.AudioOnly)
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

func verifyFile(spec FileSpec, audioOnly bool) (File, error) {
	path := strings.TrimSpace(spec.Path)
	if path == "" {
		return File{}, errors.New("output path is required")
	}
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return File{}, fmt.Errorf("normalize output path: %w", err)
	}
	if strings.HasSuffix(strings.ToLower(absolutePath), ".part") {
		return File{}, fmt.Errorf("output %q is still partial", absolutePath)
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return File{}, fmt.Errorf("stat output %q: %w", absolutePath, err)
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("output %q is not a regular file", absolutePath)
	}
	if info.Size() <= 0 {
		return File{}, fmt.Errorf("output %q is empty", absolutePath)
	}

	role := strings.ToLower(strings.TrimSpace(spec.Role))
	wantRole := "video"
	if audioOnly {
		wantRole = "audio"
	}
	if role != wantRole {
		return File{}, fmt.Errorf("output role %q does not match selection role %q", role, wantRole)
	}
	view := strings.ToLower(strings.TrimSpace(spec.View))
	switch view {
	case "left", "right", "both":
	default:
		return File{}, fmt.Errorf("unsupported output view %q", view)
	}
	container := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(spec.Container)), ".")
	if err := validateContainer(container, audioOnly); err != nil {
		return File{}, err
	}

	sha256Hex := strings.ToLower(strings.TrimSpace(spec.SHA256))
	if sha256Hex != "" && !validSHA256Hex(sha256Hex) {
		return File{}, errors.New("sha256 must be 64 lowercase hexadecimal characters")
	}
	return File{
		Path:      absolutePath,
		Role:      role,
		View:      view,
		Container: container,
		Bytes:     info.Size(),
		SHA256:    sha256Hex,
	}, nil
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
