package tuisession

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/rabesss/impartus-cli/internal/secrets"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

func (session *Session) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/health", session.health)
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/courses", session.courses)
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/events", session.streamEvents)
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/operations", session.operationsRoot)
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/operations/", session.operationByID)
	mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		writeProblem(writer, http.StatusNotFound, "not_found", "route not found")
	})
	return mux
}

func (session *Session) operationsRoot(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var operationRequest tuiproto.OperationRequest
	if err := decodeRequestJSON(writer, request, &operationRequest); err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request", "invalid operation request")
		return
	}
	operation, err := session.operations.start(operationRequest.Kind)
	if errors.Is(err, errTooManyOperations) {
		writeProblem(writer, http.StatusTooManyRequests, "operation_limit", "session operation limit reached")
		return
	}
	if errors.Is(err, errRegistryStopping) {
		writeProblem(writer, http.StatusServiceUnavailable, "session_closed", "TUI session is closed")
		return
	}
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "unsupported_operation", "unsupported operation kind")
		return
	}
	writeJSON(writer, http.StatusAccepted, operation)
}

func (session *Session) operationByID(writer http.ResponseWriter, request *http.Request) {
	identifier := strings.TrimPrefix(request.URL.Path, tuiproto.ProtocolBasePath+"/operations/")
	if identifier == "" || strings.Contains(identifier, "/") {
		writeProblem(writer, http.StatusNotFound, "operation_not_found", "operation not found")
		return
	}
	var (
		operation tuiproto.Operation
		err       error
	)
	switch request.Method {
	case http.MethodGet:
		operation, err = session.operations.get(identifier)
	case http.MethodDelete:
		operation, err = session.operations.cancelOperation(identifier)
	default:
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodDelete)
		writeProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if errors.Is(err, errOperationNotFound) {
		writeProblem(writer, http.StatusNotFound, "operation_not_found", "operation not found")
		return
	}
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "operation_failed", "operation request failed")
		return
	}
	writeJSON(writer, http.StatusOK, operation)
}

func (session *Session) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if request.Header.Get(tuiproto.ProtocolHeader) != tuiproto.ProtocolVersion {
			writer.Header().Set(tuiproto.SupportedProtocolHeader, tuiproto.ProtocolVersion)
			supported := tuiproto.ProtocolVersion
			writeJSON(writer, http.StatusUpgradeRequired, tuiproto.Problem{
				Code:              "protocol_mismatch",
				Error:             "unsupported TUI protocol",
				SupportedProtocol: &supported,
			})
			return
		}
		provided := request.Header.Get(tuiproto.CapabilityHeader)
		if subtle.ConstantTimeCompare([]byte(provided), []byte(session.capability)) != 1 {
			writeProblem(writer, http.StatusUnauthorized, "unauthorized", "invalid session capability")
			return
		}
		if session.ctx.Err() != nil {
			writeProblem(writer, http.StatusServiceUnavailable, "session_closed", "TUI session is closed")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (session *Session) health(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writeJSON(writer, http.StatusOK, tuiproto.Health{
		Protocol:  tuiproto.ProtocolVersion,
		SessionID: session.id,
		Status:    tuiproto.HealthStatusOK,
		Version:   session.version,
	})
}

func (session *Session) courses(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	courses, err := session.catalog.Courses(request.Context())
	if err != nil {
		writeProblem(writer, http.StatusBadGateway, "catalog_unavailable", "course catalog is unavailable")
		return
	}
	projected := make([]tuiproto.Course, 0, len(courses))
	for _, course := range courses {
		projected = append(projected, tuiproto.Course{
			InstituteID:   int64(course.InstituteID),
			SessionID:     int64(course.SessionID),
			SubjectID:     int64(course.SubjectID),
			SubjectName:   course.SubjectName,
			SessionName:   course.SessionName,
			ProfessorName: course.ProfessorName,
			VideoCount:    int64(course.VideoCount),
		})
	}
	writeJSON(writer, http.StatusOK, tuiproto.CourseList{Courses: projected})
}

func methodNotAllowed(writer http.ResponseWriter, allowed string) {
	writer.Header().Set("Allow", allowed)
	writeProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func writeProblem(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, tuiproto.Problem{Code: code, Error: secrets.Scrub(message)})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		return
	}
}

func decodeRequestJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("request content type must be application/json")
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain exactly one JSON object")
	}
	return nil
}
