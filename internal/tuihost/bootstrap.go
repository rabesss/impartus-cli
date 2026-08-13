package tuihost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

const bootstrapFileName = "bootstrap.json"

type bootstrapFile struct {
	directory string
	path      string
}

func createBootstrap(payload tuiproto.Bootstrap) (_ *bootstrapFile, returnErr error) {
	directory, err := os.MkdirTemp("", "impartus-tui-session-")
	if err != nil {
		return nil, fmt.Errorf("create private OpenTUI bootstrap directory: %w", err)
	}
	bootstrap := &bootstrapFile{directory: directory, path: filepath.Join(directory, bootstrapFileName)}
	keep := false
	defer func() {
		if !keep {
			returnErr = errors.Join(returnErr, bootstrap.cleanup())
		}
	}()
	if secureErr := secureBootstrapDirectory(directory); secureErr != nil {
		return nil, secureErr
	}
	file, err := os.OpenFile(bootstrap.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- path is fixed beneath a fresh private directory
	if err != nil {
		return nil, fmt.Errorf("create private OpenTUI bootstrap: %w", err)
	}
	if secureErr := secureBootstrapFile(file); secureErr != nil {
		return nil, errors.Join(secureErr, file.Close())
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if encodeErr := encoder.Encode(payload); encodeErr != nil {
		return nil, errors.Join(fmt.Errorf("write private OpenTUI bootstrap: %w", encodeErr), file.Close())
	}
	if syncErr := file.Sync(); syncErr != nil {
		return nil, errors.Join(fmt.Errorf("sync private OpenTUI bootstrap: %w", syncErr), file.Close())
	}
	if validateErr := validateBootstrapFile(file); validateErr != nil {
		return nil, errors.Join(validateErr, file.Close())
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close private OpenTUI bootstrap: %w", err)
	}
	keep = true
	return bootstrap, nil
}

func (bootstrap *bootstrapFile) consumed() (bool, error) {
	_, err := os.Lstat(bootstrap.path)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, os.ErrNotExist):
		return true, nil
	default:
		return false, fmt.Errorf("inspect private OpenTUI bootstrap consumption: %w", err)
	}
}

func (bootstrap *bootstrapFile) cleanup() error {
	if bootstrap == nil || bootstrap.directory == "" {
		return nil
	}
	if err := os.RemoveAll(bootstrap.directory); err != nil {
		return fmt.Errorf("remove private OpenTUI bootstrap directory: %w", err)
	}
	return nil
}
