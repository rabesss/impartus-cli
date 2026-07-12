package server

import (
	"encoding/json"
	"net/http"
)

// responseMeta represents the meta field in API responses
type responseMeta struct {
	Command string `json:"command"`
	Mode    string `json:"mode"`
}

// retryHint indicates whether an error is retryable and how long to wait before retrying
type retryHint struct {
	Retryable  bool `json:"retryable"`
	RetryAfter int  `json:"retryAfter"`
}

type successEnvelope struct {
	Success bool         `json:"success"`
	Data    any          `json:"data"`
	Error   any          `json:"error"`
	Meta    responseMeta `json:"meta"`
}

type errorEnvelope struct {
	Success bool         `json:"success"`
	Data    any          `json:"data"`
	Error   *errorBody   `json:"error"`
	Meta    responseMeta `json:"meta"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func respondWithEnvelope(w http.ResponseWriter, status int, command string, data any) {
	body, err := json.Marshal(successEnvelope{
		Success: true,
		Data:    data,
		Meta:    responseMeta{Command: command, Mode: "api"},
	})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body) //nolint:errcheck // nothing to do if write fails after header sent
}

func respondWithError(w http.ResponseWriter, status int, code, message, command string, hint *retryHint, details ...any) {
	body := &errorBody{Code: code, Message: message}
	if hint != nil {
		body.Details = hint
	} else if len(details) > 0 {
		body.Details = details[0]
	}

	data, err := json.Marshal(errorEnvelope{
		Success: false,
		Error:   body,
		Meta:    responseMeta{Command: command, Mode: "api"},
	})
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data) //nolint:errcheck // nothing to do if write fails after header sent
}

func respondWithSuccess(w http.ResponseWriter, command string, data any) {
	respondWithEnvelope(w, http.StatusOK, command, data)
}
