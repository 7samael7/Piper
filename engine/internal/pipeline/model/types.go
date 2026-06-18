package model

import "time"

type ProviderID string

const (
	ProviderGitHub ProviderID = "github"
	ProviderGitLab ProviderID = "gitlab"
	ProviderAzure  ProviderID = "azure"
)

type SupportLevel string

const (
	SupportSupported   SupportLevel = "supported"
	SupportPartial     SupportLevel = "partial"
	SupportUnsupported SupportLevel = "unsupported"
)

type IssueSeverity string

const (
	SeverityInfo    IssueSeverity = "info"
	SeverityWarning IssueSeverity = "warning"
	SeverityError   IssueSeverity = "error"
)

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

type TriggerInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	Type        string `json:"type,omitempty"`
}

type Trigger struct {
	Name   string         `json:"name"`
	Inputs []TriggerInput `json:"inputs,omitempty"`
}

type WorkflowSummary struct {
	ID       string       `json:"id"`
	Provider ProviderID   `json:"provider"`
	Name     string       `json:"name"`
	Path     string       `json:"path"`
	Triggers []Trigger    `json:"triggers"`
	JobCount int          `json:"jobCount"`
	Valid    bool         `json:"valid"`
	Support  SupportLevel `json:"support"`
}

type Workflow struct {
	WorkflowSummary
	RawYAML     string           `json:"rawYaml"`
	Jobs        []Job            `json:"jobs"`
	Graph       Graph            `json:"graph"`
	Validation  ValidationReport `json:"validation"`
	Unsupported []FeatureSupport `json:"unsupportedFeatures,omitempty"`
}

type Job struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Runner           string            `json:"runner,omitempty"`
	Stage            string            `json:"stage,omitempty"`
	Image            string            `json:"image,omitempty"`
	Needs            []string          `json:"needs"`
	If               string            `json:"if,omitempty"`
	Steps            []Step            `json:"steps"`
	Env              map[string]string `json:"env,omitempty"`
	Support          SupportLevel      `json:"support"`
	Unsupported      []FeatureSupport  `json:"unsupportedFeatures,omitempty"`
	ReusableWorkflow string            `json:"-"`
	HasContainer     bool              `json:"-"`
	HasServices      bool              `json:"-"`
	HasStrategy      bool              `json:"-"`
}

type Step struct {
	ID               string            `json:"id,omitempty"`
	Name             string            `json:"name"`
	Uses             string            `json:"uses,omitempty"`
	Run              string            `json:"run,omitempty"`
	Shell            string            `json:"shell,omitempty"`
	WorkingDirectory string            `json:"workingDirectory,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	With             map[string]string `json:"with,omitempty"`
	Support          SupportLevel      `json:"support"`
	Unsupported      []FeatureSupport  `json:"unsupportedFeatures,omitempty"`
}

type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ID      string       `json:"id"`
	Label   string       `json:"label"`
	Support SupportLevel `json:"support"`
}

type GraphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type ValidationIssue struct {
	Severity IssueSeverity `json:"severity"`
	Code     string        `json:"code"`
	Message  string        `json:"message"`
	Path     string        `json:"path,omitempty"`
	Support  SupportLevel  `json:"support"`
}

type FeatureSupport struct {
	Feature string       `json:"feature"`
	Path    string       `json:"path,omitempty"`
	Support SupportLevel `json:"support"`
	Message string       `json:"message"`
}

type ValidationReport struct {
	Valid    bool              `json:"valid"`
	Support  SupportLevel      `json:"support"`
	Issues   []ValidationIssue `json:"issues"`
	Features []FeatureSupport  `json:"features"`
}

type RunRequest struct {
	RepoPath     string            `json:"repoPath"`
	WorkflowPath string            `json:"workflowPath"`
	Provider     ProviderID        `json:"provider"`
	JobID        string            `json:"jobId,omitempty"`
	EventName    string            `json:"eventName"`
	Inputs       map[string]string `json:"inputs"`
	Env          map[string]string `json:"env"`
	Secrets      map[string]string `json:"secrets"`
}

type RunStartResponse struct {
	RunID string `json:"runId"`
}

type RunRecord struct {
	ID           string     `json:"id"`
	RepoPath     string     `json:"repoPath"`
	WorkflowPath string     `json:"workflowPath"`
	Provider     ProviderID `json:"provider"`
	JobID        string     `json:"jobId,omitempty"`
	EventName    string     `json:"eventName"`
	Status       RunStatus  `json:"status"`
	Conclusion   string     `json:"conclusion,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

func CombineSupport(levels ...SupportLevel) SupportLevel {
	result := SupportSupported
	for _, level := range levels {
		if level == SupportUnsupported {
			return SupportUnsupported
		}
		if level == SupportPartial {
			result = SupportPartial
		}
	}
	return result
}
