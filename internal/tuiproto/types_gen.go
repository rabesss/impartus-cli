// Code generated from protocol.schema.json. DO NOT EDIT.

package tuiproto

const (
	// ProtocolVersion is the protocol identity every session request must declare.
	ProtocolVersion = "tui/v2"

	// ProtocolBasePath is the versioned public path prefix of the session contract.
	ProtocolBasePath = "/tui/v2"

	// CapabilityHeader is a session protocol header name.
	CapabilityHeader = "X-Impartus-Capability"

	// ProtocolHeader is a session protocol header name.
	ProtocolHeader = "X-Impartus-Protocol"

	// SupportedProtocolHeader is a session protocol header name.
	SupportedProtocolHeader = "X-Impartus-Supported-Protocol"
)

// ArtifactList contains the current durable local lecture library.
type ArtifactList struct {
	// Artifacts newest first in the store order.
	Artifacts []ArtifactSummary `json:"artifacts"`
}

// ArtifactSummary is one presentation-safe local library record without
// exposing filesystem paths.
type ArtifactSummary struct {
	// Canonical logical artifact identity.
	ArtifactID string `json:"artifactId"`

	// Number of materialized files recorded for the artifact.
	FileCount int64 `json:"fileCount"`

	// Number of recorded files currently marked present.
	PresentFileCount int64 `json:"presentFileCount"`

	// UTC RFC3339 production timestamp.
	ProducedAt string `json:"producedAt"`

	// Human-facing lecture sequence number.
	Sequence int64 `json:"sequence"`

	// Lecture topic stored in the manifest.
	Topic string `json:"topic"`

	// Total recorded bytes across materialized files.
	TotalBytes int64 `json:"totalBytes"`
}

// AuthStatus is the closed upstream authentication readiness projected to
// the child without exposing credential state.
type AuthStatus string

const (
	// AuthStatusReady is the "ready" AuthStatus value.
	AuthStatusReady AuthStatus = "ready"
	// AuthStatusUnavailable is the "unavailable" AuthStatus value.
	AuthStatusUnavailable AuthStatus = "unavailable"
)

// Bootstrap is the one-use private handoff from the Go parent to its
// OpenTUI child.
type Bootstrap struct {
	// Loopback URL of this one foreground session. Never contains credentials.
	BaseURL string `json:"baseUrl"`

	// Fresh per-launch capability read once from an owner-private bootstrap
	// file.
	Capability string `json:"capability"`

	// Exact session protocol identity the child must send.
	Protocol string `json:"protocol"`

	// Opaque session identity used to reject stale bootstrap state.
	SessionID string `json:"sessionId"`
}

// Course is one read-only catalog course projected from the Go application
// service.
type Course struct {
	// Upstream institute identifier.
	InstituteID int64 `json:"instituteId"`

	// Course owner as reported upstream.
	ProfessorName string `json:"professorName"`

	// Upstream session identifier.
	SessionID int64 `json:"sessionId"`

	// Academic session label.
	SessionName string `json:"sessionName"`

	// Upstream subject identifier.
	SubjectID int64 `json:"subjectId"`

	// Human readable course name.
	SubjectName string `json:"subjectName"`

	// Lecture count advertised upstream.
	VideoCount int64 `json:"videoCount"`
}

// CourseList is the read-only catalog projection proving the frontend
// reaches live Go state.
type CourseList struct {
	// Courses in upstream order.
	Courses []Course `json:"courses"`
}

// Diagnostic is one non-blocking dependency or local-state preflight result
// already scrubbed by the Go parent.
type Diagnostic struct {
	// Safe human-readable result detail.
	Detail string `json:"detail"`

	// Stable dependency or subsystem name.
	Name string `json:"name"`

	// Presentation status such as pass, warn, or fail.
	Status string `json:"status"`
}

// DiagnosticList contains startup diagnostics owned by the Go parent.
type DiagnosticList struct {
	// Diagnostics in their stable collection order.
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Event is one ordered session event. Sequence numbers increase
// monotonically per session.
type Event struct {
	// Coalesced playback duration when known.
	// This property is optional and absent when not applicable.
	DurationSeconds *float64 `json:"durationSeconds,omitempty"`

	// Scrubbed human readable detail. Never carries upstream credentials.
	// This property is optional and absent when not applicable.
	Message *string `json:"message,omitempty"`

	// Current playback mute state when known.
	// This property is optional and absent when not applicable.
	Muted *bool `json:"muted,omitempty"`

	// Operation this event belongs to, when the event is operation scoped.
	// This property is optional and absent when not applicable.
	OperationID *string `json:"operationId,omitempty"`

	// Current playback pause state when known.
	// This property is optional and absent when not applicable.
	Paused *bool `json:"paused,omitempty"`

	// Coalesced progress percentage between 0 and 100.
	// This property is optional and absent when not applicable.
	Percent *float64 `json:"percent,omitempty"`

	// Coalesced playback position when known.
	// This property is optional and absent when not applicable.
	PositionSeconds *float64 `json:"positionSeconds,omitempty"`

	// Monotonic per-session sequence number.
	Sequence int64 `json:"sequence"`

	// Current playback speed when known.
	// This property is optional and absent when not applicable.
	Speed *float64 `json:"speed,omitempty"`

	// This property is optional and absent when not applicable.
	State *OperationState `json:"state,omitempty"`

	Type EventType `json:"type"`

	// Current playback volume percentage when known.
	// This property is optional and absent when not applicable.
	Volume *float64 `json:"volume,omitempty"`
}

// EventType names the ordered session event kinds delivered over the event
// stream.
type EventType string

const (
	// EventTypeSessionReady is the "session.ready" EventType value.
	EventTypeSessionReady EventType = "session.ready"
	// EventTypeOperationStarted is the "operation.started" EventType value.
	EventTypeOperationStarted EventType = "operation.started"
	// EventTypeOperationProgress is the "operation.progress" EventType value.
	EventTypeOperationProgress EventType = "operation.progress"
	// EventTypeOperationCompleted is the "operation.completed" EventType value.
	EventTypeOperationCompleted EventType = "operation.completed"
	// EventTypeOperationCanceled is the "operation.canceled" EventType value.
	EventTypeOperationCanceled EventType = "operation.canceled"
	// EventTypeOperationFailed is the "operation.failed" EventType value.
	EventTypeOperationFailed EventType = "operation.failed"
	// EventTypeStreamOverflow is the "stream.overflow" EventType value.
	EventTypeStreamOverflow EventType = "stream.overflow"
)

// Health is the session transport and safe upstream authentication
// readiness probe.
type Health struct {
	AuthStatus AuthStatus `json:"authStatus"`

	// Protocol identity this session speaks.
	Protocol string `json:"protocol"`

	// Opaque per-launch session identity. Not a credential.
	SessionID string `json:"sessionId"`

	Status HealthStatus `json:"status"`

	// Parent impartus build version.
	Version string `json:"version"`
}

// HealthStatus is the aggregate session readiness value. The session never
// reports which credentials are configured.
type HealthStatus string

const (
	// HealthStatusOK is the "ok" HealthStatus value.
	HealthStatusOK HealthStatus = "ok"
)

// Lecture is the presentation-safe subset of one live Impartus lecture.
type Lecture struct {
	// Classroom label reported upstream.
	ClassroomName string `json:"classroomName"`

	// Advertised lecture duration in seconds.
	DurationSeconds int64 `json:"durationSeconds"`

	// Upstream institute identifier.
	InstituteID int64 `json:"instituteId"`

	// Whether upstream marks this lecture as lacking audio.
	NoAudio bool `json:"noAudio"`

	// Lecture owner reported upstream.
	ProfessorName string `json:"professorName"`

	// Human-facing lecture sequence number.
	Sequence int64 `json:"sequence"`

	// Upstream session identifier.
	SessionID int64 `json:"sessionId"`

	// Academic session label.
	SessionName string `json:"sessionName"`

	// Lecture start time as supplied upstream.
	StartTime string `json:"startTime"`

	// Upstream subject identifier.
	SubjectID int64 `json:"subjectId"`

	// Course name reported on the lecture.
	SubjectName string `json:"subjectName"`

	// Lecture topic.
	Topic string `json:"topic"`

	// Stable upstream lecture timetable identifier.
	TTID int64 `json:"ttid"`

	// Number of camera views advertised upstream.
	Views int64 `json:"views"`
}

// LectureIdentity is the minimal authoritative identity accepted for
// lecture mutations. The Go parent re-resolves the full live lecture before
// acting.
type LectureIdentity struct {
	// Upstream institute identifier.
	InstituteID int64 `json:"instituteId"`

	// Upstream session identifier.
	SessionID int64 `json:"sessionId"`

	// Upstream subject identifier.
	SubjectID int64 `json:"subjectId"`

	// Stable upstream lecture timetable identifier.
	TTID int64 `json:"ttid"`
}

// LectureList contains the live lectures for one requested course identity.
type LectureList struct {
	// Lectures in the application service order.
	Lectures []Lecture `json:"lectures"`
}

// Operation is the handle returned when an operation is accepted or
// inspected.
type Operation struct {
	// Session-unique operation identifier.
	ID string `json:"id"`

	Kind OperationKind `json:"kind"`

	State OperationState `json:"state"`
}

// OperationKind names the operations the session may start. The bounded
// foundation exposes only a transport self test.
type OperationKind string

const (
	// OperationKindSelftest is the "selftest" OperationKind value.
	OperationKindSelftest OperationKind = "selftest"
	// OperationKindDownload is the "download" OperationKind value.
	OperationKindDownload OperationKind = "download"
	// OperationKindPlayback is the "playback" OperationKind value.
	OperationKindPlayback OperationKind = "playback"
)

// OperationRequest is the request body accepted when starting an operation.
type OperationRequest struct {
	Kind OperationKind `json:"kind"`

	// Lecture identity required by lecture-scoped operations.
	// This property is optional and absent when not applicable.
	Lecture *LectureIdentity `json:"lecture,omitempty"`

	// Whether playback should use an existing durable resume checkpoint when
	// available.
	// This property is optional and absent when not applicable.
	Resume *bool `json:"resume,omitempty"`
}

// OperationState is one operation lifecycle state. Every state except
// running is terminal.
type OperationState string

const (
	// OperationStateRunning is the "running" OperationState value.
	OperationStateRunning OperationState = "running"
	// OperationStateCompleted is the "completed" OperationState value.
	OperationStateCompleted OperationState = "completed"
	// OperationStateCanceled is the "canceled" OperationState value.
	OperationStateCanceled OperationState = "canceled"
	// OperationStateFailed is the "failed" OperationState value.
	OperationStateFailed OperationState = "failed"
)

// PlaybackCommand is one typed playback-control request. The action
// determines whether flag or value is required.
type PlaybackCommand struct {
	Action PlaybackCommandAction `json:"action"`

	// Boolean value used by pause and mute.
	// This property is optional and absent when not applicable.
	Flag *bool `json:"flag,omitempty"`

	// Numeric value used by seek, volume, and speed.
	// This property is optional and absent when not applicable.
	Value *float64 `json:"value,omitempty"`
}

// PlaybackCommandAction names one bounded mpv control owned by the Go
// playback session.
type PlaybackCommandAction string

const (
	// PlaybackCommandActionPause is the "pause" PlaybackCommandAction value.
	PlaybackCommandActionPause PlaybackCommandAction = "pause"
	// PlaybackCommandActionSeek is the "seek" PlaybackCommandAction value.
	PlaybackCommandActionSeek PlaybackCommandAction = "seek"
	// PlaybackCommandActionMute is the "mute" PlaybackCommandAction value.
	PlaybackCommandActionMute PlaybackCommandAction = "mute"
	// PlaybackCommandActionVolume is the "volume" PlaybackCommandAction value.
	PlaybackCommandActionVolume PlaybackCommandAction = "volume"
	// PlaybackCommandActionSpeed is the "speed" PlaybackCommandAction value.
	PlaybackCommandActionSpeed PlaybackCommandAction = "speed"
	// PlaybackCommandActionCycleVideo is the "cycleVideo" PlaybackCommandAction value.
	PlaybackCommandActionCycleVideo PlaybackCommandAction = "cycleVideo"
)

// Problem is the uniform session error body. It never discloses local state
// or credentials.
type Problem struct {
	// Stable machine readable failure code.
	Code string `json:"code"`

	// Short actionable failure summary.
	Error string `json:"error"`

	// Protocol identity this session speaks, sent with protocol upgrade
	// failures.
	// This property is optional and absent when not applicable.
	SupportedProtocol *string `json:"supportedProtocol,omitempty"`
}
