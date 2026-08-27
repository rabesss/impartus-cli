package cli

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

type jsonEnvelope struct {
	Success bool     `json:"success"`
	Data    any      `json:"data"`
	Error   *jsonErr `json:"error"`
	Meta    jsonMeta `json:"meta"`
}

type jsonErr struct {
	Message string `json:"message"`
}

type jsonMeta struct {
	Command  string   `json:"command"`
	Mode     string   `json:"mode"`
	Warnings []string `json:"warnings,omitempty"`
}

func newSuccessEnvelopeWithWarnings(command string, data any, warnings []string) jsonEnvelope {
	envelope := newSuccessEnvelope(command, data)
	envelope.Meta.Warnings = append([]string(nil), warnings...)
	return envelope
}

type jsonEnvelopeError struct {
	payload string
}

func (e jsonEnvelopeError) Error() string {
	return e.payload
}

type capabilityPayload struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	DefaultMode string              `json:"defaultMode"`
	Flags       []string            `json:"flags"`
	Commands    []capabilityCommand `json:"commands"`
}

type capabilityCommand struct {
	Name  string `json:"name"`
	Usage string `json:"usage"`
}

type commandHelpPayload struct {
	Command     string   `json:"command"`
	Description string   `json:"description"`
	Usage       []string `json:"usage"`
	Flags       []string `json:"flags,omitempty"`
}

type versionPayload struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	BuildDate string `json:"buildDate"`
}

func newCommandHelpPayload(help commandHelp) commandHelpPayload {
	return commandHelpPayload{
		Command:     help.command,
		Description: help.description,
		Usage:       append([]string(nil), help.usage...),
		Flags:       append([]string(nil), help.flags...),
	}
}

func stripGlobalJSONFlag(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	jsonMode := false
	command := ""
	parsingCommandFlags := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if command == "" && arg != "--json" && arg != "--" {
			command = arg
			parsingCommandFlags = true
			filtered = append(filtered, arg)
			continue
		}
		if parsingCommandFlags && commandFlagConsumesNextValue(command, arg) {
			filtered = append(filtered, arg)
			if index+1 < len(args) {
				index++
				filtered = append(filtered, args[index])
			}
			continue
		}
		if arg == "--" {
			filtered = append(filtered, args[index:]...)
			break
		}
		if arg == "--json" {
			jsonMode = true
			continue
		}
		filtered = append(filtered, arg)
		if parsingCommandFlags && (arg == "" || arg == "-" || !strings.HasPrefix(arg, "-")) {
			parsingCommandFlags = false
		}
	}
	return filtered, jsonMode
}

func commandFlagConsumesNextValue(command, argument string) bool {
	if strings.Contains(argument, "=") {
		return false
	}
	name := strings.TrimPrefix(argument, "-")
	name = strings.TrimPrefix(name, "-")
	if name == argument || name == "" {
		return false
	}

	switch command {
	case "lectures":
		return name == "subject" || name == "s" || name == "session" || name == "S"
	case "download":
		switch name {
		case "subject", "s", "session", "S", "ttid", "start", "end", "quality", "views", "format", "output", "o":
			return true
		}
	case "play":
		switch name {
		case "subject", "s", "session", "S", "start", "end", "lecture", "l", "quality", "views", "mpv-mode":
			return true
		}
	case "watch":
		switch name {
		case "subject", "s", "session", "S", "interval", "output", "o":
			return true
		}
	case "serve":
		return name == "port"
	}
	return false
}

func newSuccessEnvelope(command string, data any) jsonEnvelope {
	return jsonEnvelope{
		Success: true,
		Data:    data,
		Error:   nil,
		Meta: jsonMeta{
			Command: command,
			Mode:    "json",
		},
	}
}

func newErrorEnvelope(command string, err error) jsonEnvelope {
	return jsonEnvelope{
		Success: false,
		Data:    nil,
		Error:   &jsonErr{Message: secrets.ScrubError(err)},
		Meta: jsonMeta{
			Command: command,
			Mode:    "json",
		},
	}
}

func emitJSONEnvelope(payload jsonEnvelope) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(payload)
}

func newJSONError(command string, err error) error {
	payload, marshalErr := json.Marshal(newErrorEnvelope(command, err))
	if marshalErr != nil {
		return err
	}
	return jsonEnvelopeError{payload: string(payload)}
}

func newJSONErrorWithData(command string, data any, err error) error {
	envelope := newErrorEnvelope(command, err)
	envelope.Data = data
	payload, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		return err
	}
	return jsonEnvelopeError{payload: string(payload)}
}

func helpPayload() capabilityPayload {
	return capabilityPayload{
		Name:        "impartus",
		Description: "CLI and terminal UI for Impartus lectures",
		DefaultMode: "tui",
		Flags:       []string{"--json"},
		Commands: []capabilityCommand{
			{Name: "help", Usage: "impartus help"},
			{Name: "version", Usage: "impartus version"},
			{Name: "courses", Usage: "impartus courses"},
			{Name: "lectures", Usage: "impartus lectures --subject <id> --session <id>"},
			{Name: "download", Usage: "impartus download --subject <id> --session <id> [--ttid <id> | --start <n> --end <n>]"},
			{Name: "serve", Usage: "impartus serve [--port <port>]"},
			{Name: "play", Usage: "impartus play --subject <id> --session <id> [--lecture <n>] (not available in JSON mode)"},
			{Name: "doctor", Usage: "impartus doctor"},
			{Name: "library", Usage: "impartus library list|show|verify"},
			{Name: "watch", Usage: "impartus watch [--subject <id> --session <id>] [--once] [--dry-run] [--events]"},
			{Name: "tui", Usage: "impartus tui (not available in JSON mode)"},
		},
	}
}
