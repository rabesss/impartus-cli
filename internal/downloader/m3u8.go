package downloader

import (
	"fmt"
	"os"
	"path/filepath"
)

func writeM3U8File(path string, chunks []string) error {
	// G304: file paths are constructed from validated config and internal data
	// #nosec G304
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { closeErr := f.Close(); _ = closeErr }()

	_, err = f.WriteString(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-ALLOW-CACHE:YES
#EXT-X-TARGETDURATION:11
#EXT-X-KEY:METHOD=NONE
`)
	if err != nil {
		return err
	}

	manifestDir := filepath.Dir(path)
	for _, chunk := range chunks {
		_, err = f.WriteString("#EXTINF:1\n")
		if err != nil {
			return err
		}
		// If the chunk path is absolute or on a different volume, use it as-is;
		// otherwise compute a relative path from the manifest directory.
		chunkPath := chunk
		if !filepath.IsAbs(chunk) {
			if rel, relErr := filepath.Rel(manifestDir, chunk); relErr == nil {
				chunkPath = rel
			}
		}
		_, err = f.WriteString(chunkPath + "\n")
		if err != nil {
			return err
		}
	}

	_, err = f.WriteString("#EXT-X-ENDLIST")
	return err
}

// CreateTempM3U8File writes temporary M3U8 manifest files for each view and returns the file references.
func (d *Downloader) CreateTempM3U8File(downloadedPlaylist DownloadedPlaylist) (M3U8File, error) {
	m3u8File := M3U8File{Playlist: downloadedPlaylist.Playlist}
	// G301: 0755 is standard for user download directories
	// #nosec G301
	if err := os.MkdirAll(d.config.TempDirLocation, 0o755); err != nil {
		return m3u8File, err
	}

	if len(downloadedPlaylist.FirstViewChunks) > 0 {
		firstPath := fmt.Sprintf("%s/%d_first.m3u8", d.config.TempDirLocation, downloadedPlaylist.Playlist.ID)
		if err := writeM3U8File(firstPath, downloadedPlaylist.FirstViewChunks); err != nil {
			return m3u8File, err
		}
		m3u8File.FirstViewFile = firstPath
	}

	if len(downloadedPlaylist.SecondViewChunks) > 0 {
		secondPath := fmt.Sprintf("%s/%d_second.m3u8", d.config.TempDirLocation, downloadedPlaylist.Playlist.ID)
		if err := writeM3U8File(secondPath, downloadedPlaylist.SecondViewChunks); err != nil {
			return m3u8File, err
		}
		m3u8File.SecondViewFile = secondPath
	}

	return m3u8File, nil
}
