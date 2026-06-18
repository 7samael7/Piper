package model

import "context"

type Provider interface {
	ID() ProviderID
	Discover(ctx context.Context, repoPath string) ([]WorkflowSummary, error)
	Load(ctx context.Context, repoPath, workflowPath string) (*Workflow, []byte, error)
	Validate(ctx context.Context, workflow *Workflow) ValidationReport
}
