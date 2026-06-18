package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/pipeline-workbench/engine/internal/logs"
	"github.com/pipeline-workbench/engine/internal/pipeline/model"
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

CREATE INDEX IF NOT EXISTS idx_runs_repo_started_at ON runs(repo_path, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_run_events_run_id_sequence ON run_events(run_id, sequence);
`

type Store struct {
	db *sql.DB
}

func OpenDefault(ctx context.Context) (*Store, error) {
	path := os.Getenv("PIPELINE_WORKBENCH_DB")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".pipeline-workbench", "workbench.db")
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
