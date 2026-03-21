package downloader

import (
	"bufio"
	"regexp"
	"strings"
)

type ParsedPlaylist struct {
	KeyURL           string
	Title            string
	FirstViewURLs    []string
	SecondViewURLs   []string
	ID               int
	SeqNo            int
	HasMultipleViews bool
}

func PlaylistParser(scanner *bufio.Scanner, id int, title string, seqNo int) ParsedPlaylist {
	var parsedOutput ParsedPlaylist
	isFirstView := true
	var firstViewURLs []string
	var secondViewURLs []string

	parsedOutput.ID = id
	parsedOutput.Title = title
	parsedOutput.SeqNo = seqNo

	for scanner.Scan() {
		l := scanner.Text()
		if parsedOutput.KeyURL == "" && strings.HasPrefix(l, "#EXT-X-KEY") {
			pattern := `URI="([^"]+)"`
			re := regexp.MustCompile(pattern)
			match := re.FindStringSubmatch(l)
			if len(match) == 2 {
				parsedOutput.KeyURL = match[1]
			}
		} else if strings.HasPrefix(l, "#EXT-X-DISCONTINUITY") {
			isFirstView = false
		} else if !strings.HasPrefix(l, "#EXT") {
			if isFirstView {
				firstViewURLs = append(firstViewURLs, l)
			} else {
				secondViewURLs = append(secondViewURLs, l)
			}
		}
	}

	if isFirstView {
		parsedOutput.FirstViewURLs = firstViewURLs
		return parsedOutput
	}

	parsedOutput.HasMultipleViews = true
	parsedOutput.FirstViewURLs = firstViewURLs
	parsedOutput.SecondViewURLs = secondViewURLs
	return parsedOutput
}
