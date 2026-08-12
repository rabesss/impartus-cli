package artifact

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

func verifyContainerSignature(file *os.File, path, container string) error {
	header := make([]byte, 12)
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("read output %q: %w", path, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind output %q: %w", path, err)
	}
	if !matchesContainerSignature(header[:n], container) {
		return fmt.Errorf("output %q does not match container %q", path, container)
	}
	return nil
}

func matchesContainerSignature(header []byte, container string) bool {
	switch container {
	case "mp4", "m4a":
		return len(header) >= 8 && bytes.Equal(header[4:8], []byte("ftyp"))
	case "mkv":
		return bytes.HasPrefix(header, []byte{0x1a, 0x45, 0xdf, 0xa3})
	case "mp3":
		return bytes.HasPrefix(header, []byte("ID3")) || matchesMP3Frame(header)
	case "aac":
		return bytes.HasPrefix(header, []byte("ADIF")) || matchesADTSFrame(header)
	case "opus":
		return bytes.HasPrefix(header, []byte("OggS"))
	default:
		return false
	}
}

func matchesMP3Frame(header []byte) bool {
	return len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0
}

func matchesADTSFrame(header []byte) bool {
	return len(header) >= 2 && header[0] == 0xff && header[1]&0xf6 == 0xf0
}
