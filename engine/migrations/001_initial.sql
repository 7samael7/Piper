CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    repo_path TEXT NOT NULL,
    workflow_path TEXT NOT NULL,
    provider TEXT NOT NULL,
    job_id TEXT,
    event_name TEXT NOT NULL,
    status TEXT NOT NULL,
    conclusion TEXT,
    started_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS run_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    time TEXT NOT NULL,
    type TEXT NOT NULL,
    job_id TEXT,
    step_id TEXT,
    stream TEXT,
    status TEXT,
    message TEXT NOT NULL,
    data_json TEXT,
    FOREIGN KEY (run_id) REFERENCES runs(id)
);

CREATE TABLE IF NOT EXISTS run_jobs (
    run_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (run_id, job_id),
    FOREIGN KEY (run_id) REFERENCES runs(id)
);

CREATE TABLE IF NOT EXISTS run_steps (
    run_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    step_id TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (run_id, job_id, step_id),
    FOREIGN KEY (run_id) REFERENCES runs(id)
);

CREATE TABLE IF NOT EXISTS app_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    data_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS trusted_actions (
    repo_path TEXT NOT NULL,
    reference TEXT NOT NULL,
    resolved_sha TEXT,
    created_at TEXT NOT NULL,
    PRIMARY KEY (repo_path, reference)
);

CREATE INDEX IF NOT EXISTS idx_runs_repo_started_at ON runs(repo_path, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_run_events_run_id_sequence ON run_events(run_id, sequence);
