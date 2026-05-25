package cli

import (
	"encoding/json"
	"os"
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
	Command string `json:"command"`
	Mode    string `json:"mode"`
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

type versionPayload struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	BuildDate string `json:"buildDate"`
}

func stripGlobalJSONFlag(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	jsonMode := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, jsonMode
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
		Error:   &jsonErr{Message: err.Error()},
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

func helpPayload() capabilityPayload {
	return capabilityPayload{
		Name:        "impartus",
		Description: "CLI and interactive downloader for Impartus lectures",
		DefaultMode: "interactive",
		Flags:       []string{"--json"},
		Commands: []capabilityCommand{
			{Name: "help", Usage: "impartus help"},
			{Name: "version", Usage: "impartus version"},
			{Name: "courses", Usage: "impartus courses"},
			{Name: "lectures", Usage: "impartus lectures --subject <id> --session <id>"},
			{Name: "download", Usage: "impartus download --subject <id> --session <id> [--start <n>] [--end <n>]"},
			{Name: "serve", Usage: "impartus serve [--port <port>]"},
			{Name: "play", Usage: "impartus play --subject <id> --session <id> [--lecture <n>] (not available in JSON mode)"},
		},
	}
}
