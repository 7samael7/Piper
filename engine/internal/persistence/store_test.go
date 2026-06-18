package persistence

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/7samael7/Piper/engine/internal/logs"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestStorePersistsRunEventsAndSettings(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "piper.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	record, err := store.CreateRun(ctx, model.RunRequest{
		RepoPath: "/repo", WorkflowPath: "workflow.yml",
		Provider: model.ProviderGitHub, EventName: "push",
	})
	if err != nil {
		t.Fatal(err)
	}
	event := logs.New(record.ID, "job_status", "queued")
	event.JobID = "test"
	event.Status = model.RunQueued
	if err := store.AppendEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertExecutionStatus(ctx, event); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, record.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	settings := model.Settings{
		Concurrency: 2, MaxExpandedJobs: 64, WorkspaceMode: "isolated",
		NetworkAccess: "internal", JobTimeoutSeconds: 60, StepTimeoutSeconds: 30,
	}
	if err := store.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSettings(ctx)
	if err != nil || got.WorkspaceMode != "isolated" {
		t.Fatalf("settings=%#v err=%v", got, err)
	}
}
