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
	if name, ok := explicitHelpTarget(args); ok {
		help, ok := commandHelpByName[name]
		return help, ok
	}
	if !hasHelpBeforeSentinel(args[0], args[1:]) {
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

func explicitHelpTarget(args []string) (string, bool) {
	if len(args) < 2 || !isExplicitHelpCommand(args[0]) {
		return "", false
	}
	target := args[1:]
	if last := target[len(target)-1]; last == "--help" || last == "-h" {
		target = target[:len(target)-1]
	}
	if len(target) != 1 {
		if len(target) == 2 && target[0] == "library" {
			return strings.Join(target, "."), true
		}
		return "", false
	}
	return target[0], true
}

func explicitNestedHelpError(args []string) (bool, error) {
	if len(args) < 3 || !isExplicitHelpCommand(args[0]) || args[1] != "library" {
		return false, nil
	}
	return true, fmt.Errorf("unknown library command: %s", args[2])
}

func explicitRootHelpError(args []string) error {
	if len(args) <= 1 {
		return nil
	}
	if args[1] == "help" {
		if len(args) == 2 || len(args) == 3 && (args[2] == "--help" || args[2] == "-h") {
			return nil
		}
		return fmt.Errorf("help does not accept arguments after help: %s", strings.Join(args[2:], " "))
	}
	if args[1] != "--help" && args[1] != "-h" {
		return fmt.Errorf("unknown command: %s", args[1])
	}
	if len(args) > 2 {
		return fmt.Errorf("help does not accept arguments after %s: %s", args[1], strings.Join(args[2:], " "))
	}
	return nil
}

func isExplicitHelpCommand(argument string) bool {
	switch argument {
	case "help", "--help", "-help", "-h":
		return true
	default:
		return false
	}
}

func hasHelpBeforeSentinel(command string, args []string) bool {
	parsingCommandFlags := true
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if parsingCommandFlags && commandFlagConsumesNextValue(command, argument) {
			index++
			continue
		}
		if argument == "--" {
			return false
		}
		if argument == "--help" || argument == "-h" {
			return true
		}
		if parsingCommandFlags && (argument == "" || argument == "-" || !strings.HasPrefix(argument, "-")) {
			parsingCommandFlags = false
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
