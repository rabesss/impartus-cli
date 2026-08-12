CREATE TABLE artifacts (
    artifact_id TEXT PRIMARY KEY,
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    institute_id INTEGER NOT NULL CHECK (institute_id > 0),
    subject_id INTEGER NOT NULL CHECK (subject_id > 0),
    session_id INTEGER NOT NULL CHECK (session_id > 0),
    ttid INTEGER NOT NULL CHECK (ttid > 0),
    views TEXT NOT NULL,
    quality TEXT NOT NULL,
    audio_only INTEGER NOT NULL CHECK (audio_only IN (0, 1)),
    audio_format TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    produced_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE artifact_files (
    artifact_id TEXT NOT NULL REFERENCES artifacts(artifact_id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    role TEXT NOT NULL,
    view TEXT NOT NULL,
    container TEXT NOT NULL,
    bytes INTEGER NOT NULL CHECK (bytes > 0),
    sha256 TEXT NOT NULL DEFAULT '',
    present INTEGER NOT NULL CHECK (present IN (0, 1)),
    last_verified_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (artifact_id, path)
);

CREATE INDEX artifact_files_path_idx ON artifact_files(path);
CREATE INDEX artifact_files_presence_idx ON artifact_files(artifact_id, present);

CREATE TABLE playback (
    artifact_id TEXT PRIMARY KEY REFERENCES artifacts(artifact_id) ON DELETE CASCADE,
    position_seconds REAL NOT NULL CHECK (position_seconds >= 0),
    duration_seconds REAL NOT NULL CHECK (duration_seconds >= 0),
    completed INTEGER NOT NULL CHECK (completed IN (0, 1)),
    last_played_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE jobs (
    job_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'recoverable', 'completed', 'failed', 'canceled')),
    logical_artifact_id TEXT NOT NULL,
    completed_artifact_id TEXT REFERENCES artifacts(artifact_id),
    expected_artifact_json TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error_summary TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE INDEX jobs_status_idx ON jobs(status, updated_at);
CREATE INDEX jobs_artifact_idx ON jobs(logical_artifact_id, updated_at);
