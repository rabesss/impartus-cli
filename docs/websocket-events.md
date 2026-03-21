<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/ktechhub/doctoc)*

<!---toc start-->

* [WebSocket Events](#websocket-events)
  * [Connection](#connection)
  * [Event Format](#event-format)
  * [Event Types](#event-types)
    * [job.started](#jobstarted)
    * [job.progress](#jobprogress)
    * [job.completed](#jobcompleted)
    * [job.failed](#jobfailed)
    * [job.cancelled](#jobcancelled)
  * [Usage Example](#usage-example)
  * [Reconnection](#reconnection)
  * [Differences from Design Spec](#differences-from-design-spec)

<!---toc end-->

<!-- END doctoc generated TOC please keep comment here to allow auto update -->
# WebSocket Events

Real-time progress updates for download jobs.

> **NOTE:** This documentation describes the **actual implementation** as of 2025-02-12. The event format differs from the original design spec.

## Connection

**URL:** `ws://localhost:8080/api/v1/ws`

> **IMPORTANT:** The WebSocket path is `/api/v1/ws`, not `/ws`.

**Authentication:** Required via header:

```
Authorization: Bearer <token>
```

**Example (Node.js with `ws` package):**

```javascript
import WebSocket from 'ws';

const ws = new WebSocket('ws://localhost:8080/api/v1/ws', {
  headers: {
    Authorization: `Bearer ${token}`
  }
});

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  handleEvent(data);
};

ws.onerror = (error) => {
  console.error('WebSocket error:', error);
};

ws.onclose = () => {
  console.log('WebSocket closed');
};
```

---

## Event Format

All events follow this structure:

```json
{
  "type": "event.type",
  "jobId": "job-identifier",
  "timestamp": 1705339200,
  "...": "additional fields vary by event type"
}
```

| Field | Type | Description |
|--------|-------|-------------|
| `type` | string | Event type identifier |
| `jobId` | string | Job ID (Unix timestamp-based) |
| `timestamp` | integer | Unix timestamp (seconds since epoch) |

> **NOTE:** Unlike the design spec, events do **not** have a `data` wrapper. Additional fields are placed at the top level. The timestamp is a Unix integer, not ISO 8601 string.

---

## Event Types

### job.started

Emitted when a job begins execution.

**Actual Event Format:**

```json
{
  "type": "job.started",
  "jobId": "job-1234567890",
  "status": "running",
  "timestamp": 1705339200
}
```

### job.progress

Emitted periodically as download progresses.

**Actual Event Format:**

```json
{
  "type": "job.progress",
  "jobId": "job-1234567890",
  "progress": 50,
  "status": "running",
  "phase": "downloading",
  "details": {
    "completedLectures": 5,
    "totalLectures": 10
  },
  "timestamp": 1705339500
}
```

| Field | Type | Description |
|--------|--------|-------------|
| `progress` | number | Progress value (0-100) |
| `status` | string | Job status ("running") |
| `phase` | string | Current phase (`initializing`, `logging_in`, `fetching_lectures`, `downloading_slides`, `fetching_playlists`, `downloading`) |
| `details` | object | Optional phase-specific metadata |

---

### job.completed

Emitted when all lectures finish successfully.

**Actual Event Format:**

```json
{
  "type": "job.completed",
  "jobId": "job-1234567890",
  "status": "completed",
  "progress": 100,
  "outputs": [
    "./downloads/lecture-01.mp4",
    "./downloads/lecture-02.mp4"
  ],
  "timestamp": 1705342200
}
```

### job.failed

Emitted when a job errors and cannot continue.

**Actual Event Format:**

```json
{
  "type": "job.failed",
  "jobId": "job-1234567890",
  "status": "failed",
  "error": "lecture 003: download failed",
  "timestamp": 1705342100
}
```

---

### job.cancelled

Emitted when a job is cancelled.

**Actual Event Format:**

```json
{
  "type": "job.cancelled",
  "jobId": "job-1234567890",
  "status": "cancelled",
  "progress": 42,
  "timestamp": 1705342000
}
```

`progress` may be omitted on some cancellation paths.

---

## Usage Example

```javascript
import WebSocket from 'ws';

const ws = new WebSocket('ws://localhost:8080/api/v1/ws', {
  headers: {
    Authorization: `Bearer ${token}`
  }
});

const eventHandlers = {
  'job.started': (data) => {
    console.log(`Job ${data.jobId} started`);
    showProgressIndicator();
  },

  'job.progress': (data) => {
    console.log(`Progress: ${data.progress}% [${data.phase}]`);
    if (data.details) {
      console.log('Details:', data.details);
    }
    updateProgressBar(data.progress);
  },

  'job.completed': (data) => {
    console.log(`Job ${data.jobId} complete with ${data.outputs.length} output(s)`);
    hideProgressIndicator();
  },

  'job.failed': (data) => {
    console.error(`Job ${data.jobId} failed: ${data.error}`);
    hideProgressIndicator();
  },

  'job.cancelled': (data) => {
    console.warn(`Job ${data.jobId} cancelled`);
    hideProgressIndicator();
  }
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);

  const handler = eventHandlers[message.type];
  if (handler) {
    handler(message);
  }
};
```

---

## Reconnection

If the WebSocket connection drops, implement reconnection logic:

```javascript
let reconnectAttempts = 0;
const maxReconnectAttempts = 5;

function connect() {
  const ws = new WebSocket('ws://localhost:8080/api/v1/ws');

  ws.onclose = () => {
    if (reconnectAttempts < maxReconnectAttempts) {
      reconnectAttempts++;
      console.log(`Reconnecting... (${reconnectAttempts}/${maxReconnectAttempts})`);
      setTimeout(() => connect(), 1000 * reconnectAttempts);
    }
  };

  return ws;
}

const ws = connect();
```

---

## Differences from Design Spec

| Aspect | Design Spec | Actual Implementation |
|--------|-------------|----------------------|
| WebSocket path | `/ws` | `/api/v1/ws` |
| Event data wrapper | All events have `data` object | No `data` wrapper |
| Timestamp format | ISO 8601 string | Unix integer |
| `job.started` data | Includes `courseId` | `status` only (plus common fields) |
| `job.progress` data | `lecture`, `total`, `percent`, `topic`, `seqNo` | `progress`, `status`, `phase`, optional `details` |
| `job.completed` data | `files`, `totalBytes` | `status`, `progress`, `outputs` |
| `job.lecture.complete` | Documented | Not emitted |
| `job.failed` | Documented | Emitted with `error` |
| `job.cancelled` | Documented | Emitted |
