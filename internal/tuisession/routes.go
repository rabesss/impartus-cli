package tuisession

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/secrets"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

func (session *Session) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/health", session.health)
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/courses", session.courses)
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/diagnostics", session.diagnosticsView)
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/lectures", session.lecturesView)
	mux.HandleFunc(tuiproto.ProtocolBasePath+"/library", session.libraryView)
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
	var (
		operation tuiproto.Operation
		err       error
	)
	switch operationRequest.Kind {
	case tuiproto.OperationKindSelftest:
		if operationRequest.Lecture != nil {
			writeProblem(writer, http.StatusBadRequest, "invalid_request", "self-test does not accept a lecture")
			return
		}
		operation, err = session.operations.startSelfTest()
	case tuiproto.OperationKindDownload:
		lecture, resolveErr := session.resolveLecture(request.Context(), operationRequest.Lecture)
		if resolveErr != nil {
			writeProblem(writer, http.StatusNotFound, "lecture_not_found", "lecture is unavailable")
			return
		}
		operation, err = session.operations.startDownload(lecture)
	case tuiproto.OperationKindPlayback:
		lecture, resolveErr := session.resolveLecture(request.Context(), operationRequest.Lecture)
		if resolveErr != nil {
			writeProblem(writer, http.StatusNotFound, "lecture_not_found", "lecture is unavailable")
			return
		}
		resume := operationRequest.Resume != nil && *operationRequest.Resume
		operation, err = session.operations.startPlayback(lecture, resume)
	default:
		err = errors.New("unsupported operation kind")
	}
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

func (session *Session) resolveLecture(ctx context.Context, identity *tuiproto.LectureIdentity) (client.Lecture, error) {
	if session.lectures == nil || identity == nil || identity.InstituteID <= 0 || identity.SubjectID <= 0 || identity.SessionID <= 0 || identity.TTID <= 0 {
		return client.Lecture{}, errors.New("lecture identity is invalid")
	}
	course := client.Course{
		InstituteID: int(identity.InstituteID),
		SubjectID:   int(identity.SubjectID),
		SessionID:   int(identity.SessionID),
	}
	lectures, err := session.lectures.Lectures(ctx, course)
	if err != nil {
		return client.Lecture{}, err
	}
	for _, lecture := range lectures {
		if lecture.InstituteID == course.InstituteID && lecture.SubjectID == course.SubjectID && lecture.SessionID == course.SessionID && lecture.TTID == int(identity.TTID) {
			return lecture, nil
		}
	}
	return client.Lecture{}, errors.New("lecture not found")
}

func (session *Session) operationByID(writer http.ResponseWriter, request *http.Request) {
	suffix := strings.TrimPrefix(request.URL.Path, tuiproto.ProtocolBasePath+"/operations/")
	parts := strings.Split(suffix, "/")
	if len(parts) == 2 && parts[1] == "commands" {
		session.playbackCommand(writer, request, parts[0])
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		writeProblem(writer, http.StatusNotFound, "operation_not_found", "operation not found")
		return
	}
	identifier := parts[0]
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

func (session *Session) playbackCommand(writer http.ResponseWriter, request *http.Request, identifier string) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	if identifier == "" {
		writeProblem(writer, http.StatusNotFound, "operation_not_found", "operation not found")
		return
	}
	var command tuiproto.PlaybackCommand
	if err := decodeRequestJSON(writer, request, &command); err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request", "invalid playback command")
		return
	}
	if err := session.operations.controlPlayback(request.Context(), identifier, command); err != nil {
		if errors.Is(err, errOperationNotFound) {
			writeProblem(writer, http.StatusNotFound, "operation_not_found", "active playback operation not found")
			return
		}
		writeProblem(writer, http.StatusBadRequest, "invalid_playback_command", "playback command was rejected")
		return
	}
	operation, err := session.operations.get(identifier)
	if err != nil {
		writeProblem(writer, http.StatusNotFound, "operation_not_found", "operation not found")
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
			SubjectName:   safePresentationText(course.SubjectName),
			SessionName:   safePresentationText(course.SessionName),
			ProfessorName: safePresentationText(course.ProfessorName),
			VideoCount:    int64(course.VideoCount),
		})
	}
	writeJSON(writer, http.StatusOK, tuiproto.CourseList{Courses: projected})
}

func (session *Session) diagnosticsView(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writeJSON(writer, http.StatusOK, tuiproto.DiagnosticList{
		Diagnostics: session.diagnostics,
	})
}

func (session *Session) lecturesView(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if session.lectures == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "lectures_unavailable", "lecture catalog is unavailable")
		return
	}
	course, ok := requestedCourse(request)
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_course", "course identity is invalid")
		return
	}
	lectures, err := session.lectures.Lectures(request.Context(), course)
	if err != nil {
		writeProblem(writer, http.StatusBadGateway, "lectures_unavailable", "lecture catalog is unavailable")
		return
	}
	projected := make([]tuiproto.Lecture, 0, len(lectures))
	for _, lecture := range lectures {
		projected = append(projected, projectLecture(lecture))
	}
	writeJSON(writer, http.StatusOK, tuiproto.LectureList{Lectures: projected})
}

func (session *Session) libraryView(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if session.artifacts == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "library_unavailable", "local lecture library is unavailable")
		return
	}
	records, err := session.artifacts.Artifacts(request.Context())
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "library_unavailable", "local lecture library is unavailable")
		return
	}
	artifacts := make([]tuiproto.ArtifactSummary, 0, len(records))
	for _, record := range records {
		summary := tuiproto.ArtifactSummary{
			ArtifactID: record.Manifest.ArtifactID,
			FileCount:  int64(len(record.Files)),
			ProducedAt: record.Manifest.ProducedAt.UTC().Format(time.RFC3339),
			Sequence:   int64(record.Manifest.Lecture.SeqNo),
			Topic:      safePresentationText(record.Manifest.Lecture.Topic),
		}
		for _, file := range record.Files {
			summary.TotalBytes += file.Bytes
			if file.Present {
				summary.PresentFileCount++
			}
		}
		artifacts = append(artifacts, summary)
	}
	writeJSON(writer, http.StatusOK, tuiproto.ArtifactList{Artifacts: artifacts})
}

func requestedCourse(request *http.Request) (client.Course, bool) {
	query := request.URL.Query()
	if len(query) != 3 {
		return client.Course{}, false
	}
	instituteID, instituteOK := onePositiveInteger(query["instituteId"])
	subjectID, subjectOK := onePositiveInteger(query["subjectId"])
	sessionID, sessionOK := onePositiveInteger(query["sessionId"])
	if !instituteOK || !subjectOK || !sessionOK {
		return client.Course{}, false
	}
	return client.Course{InstituteID: instituteID, SubjectID: subjectID, SessionID: sessionID}, true
}

func onePositiveInteger(values []string) (int, bool) {
	if len(values) != 1 {
		return 0, false
	}
	value, err := strconv.Atoi(values[0])
	return value, err == nil && value > 0
}

func projectLecture(lecture client.Lecture) tuiproto.Lecture {
	return tuiproto.Lecture{
		ClassroomName:   safePresentationText(lecture.ClassroomName),
		DurationSeconds: int64(lecture.ActualDuration),
		InstituteID:     int64(lecture.InstituteID),
		NoAudio:         lecture.NoAudio == 1,
		ProfessorName:   safePresentationText(lecture.ProfessorName),
		Sequence:        int64(lecture.SeqNo),
		SessionID:       int64(lecture.SessionID),
		SessionName:     safePresentationText(lecture.SessionName),
		StartTime:       safePresentationText(lecture.StartTime),
		SubjectID:       int64(lecture.SubjectID),
		SubjectName:     safePresentationText(lecture.SubjectName),
		Topic:           safePresentationText(lecture.Topic),
		TTID:            int64(lecture.TTID),
		Views:           int64(lecture.Views),
	}
}

func projectDiagnostics(diagnostics []Diagnostic) []tuiproto.Diagnostic {
	projected := make([]tuiproto.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		projected = append(projected, tuiproto.Diagnostic{
			Detail: safePresentationText(diagnostic.Detail),
			Name:   safePresentationText(diagnostic.Name),
			Status: safePresentationText(diagnostic.Status),
		})
	}
	return projected
}

func safePresentationText(value string) string {
	value = secrets.Scrub(value)
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return character
	}, value)
	return strings.Join(strings.Fields(value), " ")
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
