package cli

import (
	"fmt"
	"io"
	"strings"
)

type commandHelp struct {
	command     string
	description string
	usage       []string
	flags       []string
}

var downloadCommandHelpFlags = []string{
	"--subject,-s <id>    Subject ID",
	"--session,-S <id>    Session ID",
	"--ttid <id>          Exact lecture TTID; cannot be combined with --start/--end",
	"--start <n>          Start lecture index (1-based)",
	"--end <n>            End lecture index (1-based)",
	"--quality <value>    Quality override: 144, 450, or 720",
	"--views <value>      View override: first, second, both, left, or right",
	"--audio-only         Enable audio-only mode",
	"--format <value>     Audio format override: mp3, m4a, aac, or opus",
	"--output,-o <path>   Output directory",
	"--skip-no-audio      Skip lectures with no audio track",
	"--include-noaudio    Include lectures with no audio track",
	"--events             Emit newline-delimited JSON lifecycle events",
	"--json               Emit one JSON result envelope",
	"--help,-h            Show command help",
}

var commandHelpByName = map[string]commandHelp{
	"version": {
		command:     "version",
		description: "Show version and build date.",
		usage:       []string{"impartus version"},
	},
	"courses": {
		command:     "courses",
		description: "List available courses as JSON.",
		usage:       []string{"impartus courses"},
	},
	"lectures": {
		command:     "lectures",
		description: "List lectures for one subject and session as JSON.",
		usage:       []string{"impartus lectures --subject <id> --session <id>"},
	},
	"download": {
		command:     "download",
		description: "Download lectures and record completed media in the local library.",
		usage: []string{
			"impartus download --subject <id> --session <id> [--ttid <id> | --start <n> --end <n>] [flags]",
		},
		flags: downloadCommandHelpFlags,
	},
	"play": {
		command:     "play",
		description: "Play lectures in mpv.",
		usage:       []string{"impartus play [--subject <id> --session <id>] [flags]"},
	},
	"doctor": {
		command:     "doctor",
		description: "Check local dependencies and private paths.",
		usage:       []string{"impartus doctor"},
	},
	"library": {
		command:     "library",
		description: "Inspect and verify the local lecture library.",
		usage: []string{
			"impartus library list",
			"impartus library show <artifact-id>",
			"impartus library verify [--hash] [artifact-id]",
		},
	},
	"library.list": {
		command:     "library.list",
		description: "List recorded library artifacts.",
		usage:       []string{"impartus library list"},
	},
	"library.show": {
		command:     "library.show",
		description: "Show one recorded library artifact.",
		usage:       []string{"impartus library show <artifact-id>"},
	},
	"library.verify": {
		command:     "library.verify",
		description: "Verify recorded library artifacts and files.",
		usage:       []string{"impartus library verify [--hash] [artifact-id]"},
	},
	"watch": {
		command:     "watch",
		description: "Poll and durably download new lectures.",
		usage:       []string{"impartus watch [--subject <id> --session <id>] [--once] [--dry-run] [flags]"},
	},
	"serve": {
		command:     "serve",
		description: "Start the HTTP API server.",
		usage:       []string{"impartus serve [--port <port>]"},
	},
	"tui": {
		command:     "tui",
		description: "Launch the interactive terminal workspace.",
		usage:       []string{"impartus tui"},
	},
}

func resolveCommandHelp(args []string) (commandHelp, bool) {
	if len(args) == 0 {
		return commandHelp{}, false
	}
	if args[0] == "help" && len(args) == 2 {
		help, ok := commandHelpByName[args[1]]
		return help, ok
	}
	if !hasHelpBeforeSentinel(args[1:]) {
		return commandHelp{}, false
	}
	name := args[0]
	switch name {
	case "--version", "-version", "-v":
		name = "version"
	}
	if name == "library" && len(args) > 1 {
		if _, ok := commandHelpByName["library."+args[1]]; ok {
			name = "library." + args[1]
		} else if !strings.HasPrefix(args[1], "-") {
			return commandHelp{}, false
		}
	}
	help, ok := commandHelpByName[name]
	return help, ok
}

func hasHelpBeforeSentinel(args []string) bool {
	for _, argument := range args {
		if argument == "--" {
			return false
		}
		if argument == "--help" || argument == "-h" {
			return true
		}
	}
	return false
}

func showCommandHelp(output io.Writer, version, date string, help commandHelp) error {
	if err := showVersionTo(output, version, date); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "\n%s\n\nUsage:\n", help.description); err != nil {
		return err
	}
	for _, usage := range help.usage {
		if _, err := fmt.Fprintf(output, "  %s\n", usage); err != nil {
			return err
		}
	}
	if len(help.flags) > 0 {
		if _, err := fmt.Fprintln(output, "\nFlags:"); err != nil {
			return err
		}
		for _, flag := range help.flags {
			if _, err := fmt.Fprintf(output, "  %s\n", flag); err != nil {
				return err
			}
		}
	}
	return nil
}
