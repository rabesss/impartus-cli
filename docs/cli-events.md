# CLI NDJSON Events

`impartus download --events` and `impartus watch --events` write one JSON object
per line to stdout. This mode is intended for local automation that needs a
durable completed-artifact boundary. It is mutually exclusive with the global
`--json` response envelope.

## Envelope

Every record contains:

| Field | Type | Meaning |
|-------|------|---------|
| `schemaVersion` | integer | `1` |
| `type` | string | Lifecycle event name |
| `jobId` | string | Opaque `job-` identifier for this command run |
| `command` | string | `download` or `watch` |
| `timestamp` | string | UTC RFC 3339 timestamp |

Optional `status`, `target`, `lecture`, `artifact`, `outputs`, `details`, and
`error` fields carry event-specific data. URLs, credentials, playlist keys, and
provider routing are never part of this contract.

## Event order

A successful stream begins with `job.started`, may contain zero or more
lecture/artifact events, and ends with exactly one `job.completed`. A failed or
canceled stream ends with exactly one `job.failed` or `job.canceled`. The writer
rejects any record after a terminal event and synchronously reports output
errors to the command.

Successful commands exit 0, failures exit 1, and signal cancellation emits
`job.canceled` before exiting 130. If stdout rejects the terminal write, the
command exits 1 and does not loop trying to write another terminal record.

Watch can emit:

- `lecture.discovered`
- `lecture.started`
- `lecture.skipped` with `artifact_committed` or `cycle_budget`
- `lecture.failed`
- `artifact.committed`
- `cycle.completed`

One-shot download emits `artifact.committed` only after the manifest batch has
been recorded in the local library. If media was published but that commit did
not complete, events mode fails closed with `job.failed`; the existing human and
single-envelope JSON compatibility modes retain their warning behavior.

## Committed artifact example

```json
{"schemaVersion":1,"type":"artifact.committed","jobId":"job-...","command":"watch","timestamp":"2026-08-09T12:00:00Z","target":{"subjectId":123,"sessionId":456},"lecture":{"ttid":789,"seqNo":4,"topic":"Graph traversal"},"artifact":{"schemaVersion":1,"artifactId":"impartus:v1:...","lecture":{"ttid":789,"instituteId":10,"subjectId":123,"sessionId":456,"seqNo":4,"topic":"Graph traversal","startTime":"2026-08-09T09:00:00Z","durationSeconds":3600,"professor":"Ada","institute":"Example Institute","noAudio":false},"selection":{"views":"left","quality":"144","audioOnly":true,"audioFormat":"mp3"},"files":[{"path":"/home/user/lectures/Graph traversal.mp3","role":"audio","view":"left","container":"mp3","bytes":1048576}],"producedAt":"2026-08-09T12:00:00Z","producer":{"name":"impartus","version":"dev"}}}
```

Consumers should treat `artifact.artifactId` as the logical identity and each
`artifact.files[].path` as a verified local materialization. They must not infer
completion from `lecture.started`, an output filename, or a non-terminal cycle
event.
