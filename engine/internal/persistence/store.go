package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/7samael7/Piper/engine/internal/logs"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const schema = `
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
`

type Store struct {
	db *sql.DB
}

func OpenDefault(ctx context.Context) (*Store, error) {
	path := os.Getenv("PIPER_DB")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".piper", "piper.db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return Open(ctx, path)
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateRun(ctx context.Context, request model.RunRequest) (model.RunRecord, error) {
	now := time.Now().UTC()
	record := model.RunRecord{
		ID:           uuid.NewString(),
		RepoPath:     request.RepoPath,
		WorkflowPath: request.WorkflowPath,
		Provider:     request.Provider,
		JobID:        request.JobID,
		EventName:    request.EventName,
		Status:       model.RunQueued,
		StartedAt:    now,
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO runs (id, repo_path, workflow_path, provider, job_id, event_name, status, started_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, record.ID, record.RepoPath, record.WorkflowPath, string(record.Provider), nullable(record.JobID), record.EventName, string(record.Status), formatTime(now))
	return record, err
}

func (s *Store) UpdateRunStatus(ctx context.Context, runID string, status model.RunStatus, conclusion string, completed bool) error {
	if completed {
		now := time.Now().UTC()
		_, err := s.db.ExecContext(ctx, `
            UPDATE runs
            SET status = ?, conclusion = ?, completed_at = ?
            WHERE id = ?
        `, string(status), nullable(conclusion), formatTime(now), runID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
        UPDATE runs
        SET status = ?, conclusion = ?
        WHERE id = ?
    `, string(status), nullable(conclusion), runID)
	return err
}

func (s *Store) AppendEvent(ctx context.Context, event logs.Event) error {
	var dataJSON sql.NullString
	if len(event.Data) > 0 {
		bytes, err := json.Marshal(event.Data)
		if err != nil {
			return err
		}
		dataJSON = sql.NullString{String: string(bytes), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO run_events (run_id, time, type, job_id, step_id, stream, status, message, data_json)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, event.RunID, formatTime(event.Time), event.Type, nullable(event.JobID), nullable(event.StepID), nullable(event.Stream), nullable(string(event.Status)), event.Message, dataJSON)
	return err
}

func (s *Store) UpsertExecutionStatus(ctx context.Context, event logs.Event) error {
	if event.JobID == "" || event.Status == "" {
		return nil
	}
	now := formatTime(event.Time)
	if event.StepID != "" {
		_, err := s.db.ExecContext(ctx, `
            INSERT INTO run_steps (run_id, job_id, step_id, status, message, updated_at)
            VALUES (?, ?, ?, ?, ?, ?)
            ON CONFLICT(run_id, job_id, step_id) DO UPDATE SET
                status = excluded.status, message = excluded.message, updated_at = excluded.updated_at
        `, event.RunID, event.JobID, event.StepID, string(event.Status), event.Message, now)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO run_jobs (run_id, job_id, status, message, updated_at)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(run_id, job_id) DO UPDATE SET
            status = excluded.status, message = excluded.message, updated_at = excluded.updated_at
    `, event.RunID, event.JobID, string(event.Status), event.Message, now)
	return err
}

func (s *Store) ListRuns(ctx context.Context, repoPath string, limit int) ([]model.RunRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var (
		rows *sql.Rows
		err  error
	)
	if repoPath == "" {
		rows, err = s.db.QueryContext(ctx, `
            SELECT id, repo_path, workflow_path, provider, COALESCE(job_id, ''), event_name, status,
                   COALESCE(conclusion, ''), started_at, COALESCE(completed_at, '')
            FROM runs
            ORDER BY started_at DESC
            LIMIT ?
        `, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
            SELECT id, repo_path, workflow_path, provider, COALESCE(job_id, ''), event_name, status,
                   COALESCE(conclusion, ''), started_at, COALESCE(completed_at, '')
            FROM runs
            WHERE repo_path = ?
            ORDER BY started_at DESC
            LIMIT ?
        `, repoPath, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []model.RunRecord{}
	for rows.Next() {
		var (
			record      model.RunRecord
			provider    string
			status      string
			startedAt   string
			completedAt string
		)
		if err := rows.Scan(&record.ID, &record.RepoPath, &record.WorkflowPath, &provider, &record.JobID, &record.EventName, &status, &record.Conclusion, &startedAt, &completedAt); err != nil {
			return nil, err
		}
		record.Provider = model.ProviderID(provider)
		record.Status = model.RunStatus(status)
		record.StartedAt = parseTime(startedAt)
		if completedAt != "" {
			parsed := parseTime(completedAt)
			record.CompletedAt = &parsed
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) GetRun(ctx context.Context, runID string) (model.RunRecord, error) {
	var (
		record      model.RunRecord
		provider    string
		status      string
		startedAt   string
		completedAt string
	)
	err := s.db.QueryRowContext(ctx, `
        SELECT id, repo_path, workflow_path, provider, COALESCE(job_id, ''), event_name, status,
               COALESCE(conclusion, ''), started_at, COALESCE(completed_at, '')
        FROM runs WHERE id = ?
    `, runID).Scan(&record.ID, &record.RepoPath, &record.WorkflowPath, &provider, &record.JobID,
		&record.EventName, &status, &record.Conclusion, &startedAt, &completedAt)
	if err != nil {
		return model.RunRecord{}, err
	}
	record.Provider = model.ProviderID(provider)
	record.Status = model.RunStatus(status)
	record.StartedAt = parseTime(startedAt)
	if completedAt != "" {
		value := parseTime(completedAt)
		record.CompletedAt = &value
	}
	return record, nil
}

func (s *Store) ListEvents(ctx context.Context, runID string) ([]logs.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT time, type, COALESCE(job_id, ''), COALESCE(step_id, ''), COALESCE(stream, ''),
               COALESCE(status, ''), message, COALESCE(data_json, '')
        FROM run_events WHERE run_id = ? ORDER BY sequence
    `, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []logs.Event{}
	for rows.Next() {
		var event logs.Event
		var timestamp, status, dataJSON string
		if err := rows.Scan(&timestamp, &event.Type, &event.JobID, &event.StepID, &event.Stream, &status, &event.Message, &dataJSON); err != nil {
			return nil, err
		}
		event.RunID = runID
		event.Time = parseTime(timestamp)
		event.Status = model.RunStatus(status)
		if dataJSON != "" {
			_ = json.Unmarshal([]byte(dataJSON), &event.Data)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) GetSettings(ctx context.Context) (model.Settings, error) {
	settings := model.Settings{
		Concurrency: 4, MaxExpandedJobs: 128,
		WorkspaceMode: "writable", NetworkAccess: "enabled",
		JobTimeoutSeconds: 3600, StepTimeoutSeconds: 1800,
	}
	var data string
	err := s.db.QueryRowContext(ctx, "SELECT data_json FROM app_settings WHERE id = 1").Scan(&data)
	if err == sql.ErrNoRows {
		return settings, nil
	}
	if err != nil {
		return model.Settings{}, err
	}
	if err := json.Unmarshal([]byte(data), &settings); err != nil {
		return model.Settings{}, err
	}
	return settings, nil
}

func (s *Store) UpdateSettings(ctx context.Context, settings model.Settings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
        INSERT INTO app_settings (id, data_json) VALUES (1, ?)
        ON CONFLICT(id) DO UPDATE SET data_json = excluded.data_json
    `, string(data))
	return err
}

func (s *Store) ListTrust(ctx context.Context, repoPath string) ([]map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT repo_path, reference, COALESCE(resolved_sha, ''), created_at
        FROM trusted_actions WHERE repo_path = ? ORDER BY reference
    `, repoPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]string{}
	for rows.Next() {
		var repo, reference, sha, created string
		if err := rows.Scan(&repo, &reference, &sha, &created); err != nil {
			return nil, err
		}
		result = append(result, map[string]string{
			"repoPath": repo, "reference": reference, "resolvedSha": sha, "createdAt": created,
		})
	}
	return result, rows.Err()
}

func (s *Store) UpdateTrust(ctx context.Context, repoPath, reference, sha string, trusted bool) error {
	if !trusted {
		_, err := s.db.ExecContext(ctx, "DELETE FROM trusted_actions WHERE repo_path = ? AND reference = ?", repoPath, reference)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO trusted_actions (repo_path, reference, resolved_sha, created_at)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(repo_path, reference) DO UPDATE SET resolved_sha = excluded.resolved_sha
    `, repoPath, reference, nullable(sha), formatTime(time.Now().UTC()))
	return err
}

func nullable(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
