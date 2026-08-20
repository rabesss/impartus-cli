package cli

import (
	"errors"
	"flag"
	"io"
)

type downloadFlags struct {
	subject        int
	session        int
	ttid           int
	start          int
	end            int
	ttidSet        bool
	startSet       bool
	endSet         bool
	quality        string
	views          string
	audioOnly      bool
	format         string
	output         string
	skipNoAudio    bool
	includeNoAudio bool
	events         bool
}

func parseDownloadFlags(args []string) (downloadFlags, error) {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f downloadFlags
	fs.IntVar(&f.subject, "subject", 0, "Subject ID")
	fs.IntVar(&f.subject, "s", 0, "Subject ID")
	fs.IntVar(&f.session, "session", 0, "Session ID")
	fs.IntVar(&f.session, "S", 0, "Session ID")
	fs.IntVar(&f.ttid, "ttid", 0, "Exact lecture TTID")
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
	fs.BoolVar(&f.events, "events", false, "Emit newline-delimited JSON lifecycle events")

	if err := fs.Parse(args); err != nil {
		return downloadFlags{}, err
	}
	if fs.NArg() > 0 {
		return downloadFlags{}, errors.New("download does not accept positional arguments")
	}
	fs.Visit(func(flag *flag.Flag) {
		switch flag.Name {
		case "ttid":
			f.ttidSet = true
		case "start":
			f.startSet = true
		case "end":
			f.endSet = true
		}
	})
	if f.subject <= 0 || f.session <= 0 {
		return downloadFlags{}, errors.New("download requires --subject/-s and --session/-S")
	}
	if f.ttidSet && f.ttid <= 0 {
		return downloadFlags{}, errors.New("download --ttid must be positive")
	}
	if f.ttidSet && (f.startSet || f.endSet) {
		return downloadFlags{}, errors.New("download --ttid cannot be combined with --start/--end")
	}
	return f, nil
}
