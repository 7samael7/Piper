package model

import (
	"context"
	"time"
)

type ProviderID string

const (
	ProviderCommon ProviderID = "common"
	ProviderGitHub ProviderID = "github"
	ProviderGitLab ProviderID = "gitlab"
	ProviderAzure  ProviderID = "azure"
)

type SupportLevel string

const (
	SupportSupportedLocal  SupportLevel = "supported-local"
	SupportEmulated        SupportLevel = "emulated"
	SupportPartial         SupportLevel = "partial"
	SupportValidationOnly  SupportLevel = "validation-only"
	SupportUnsupported     SupportLevel = "unsupported"
	SupportRequiresConsent SupportLevel = "requires-consent"

	SupportSupported = SupportSupportedLocal
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
	RunSkipped   RunStatus = "skipped"
)

type ConditionSpec struct {
	Provider ProviderID `json:"provider"`
	Original string     `json:"original"`
	Kind     string     `json:"kind,omitempty"`
}

type EvaluationError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Position int    `json:"position,omitempty"`
}

type ConditionResult struct {
	Expression string           `json:"expression"`
	Evaluated  bool             `json:"evaluated"`
	Value      bool             `json:"value"`
	Reason     string           `json:"reason,omitempty"`
	Error      *EvaluationError `json:"error,omitempty"`
}

type MatrixSpec struct {
	Dimensions  map[string][]interface{}     `json:"dimensions,omitempty"`
	Include     []map[string]interface{}     `json:"include,omitempty"`
	Exclude     []map[string]interface{}     `json:"exclude,omitempty"`
	AzureLegs   map[string]map[string]string `json:"azureLegs,omitempty"`
	FailFast    bool                         `json:"failFast"`
	MaxParallel int                          `json:"maxParallel,omitempty"`
}

type ServiceSpec struct {
	Name           string            `json:"name"`
	Image          string            `json:"image"`
	Env            map[string]string `json:"env,omitempty"`
	Ports          []string          `json:"ports,omitempty"`
	Aliases        []string          `json:"aliases,omitempty"`
	Options        string            `json:"options,omitempty"`
	StartupTimeout int               `json:"startupTimeoutSeconds,omitempty"`
}

type ArtifactSpec struct {
	Name     string   `json:"name"`
	Paths    []string `json:"paths"`
	When     string   `json:"when,omitempty"`
	ExpireIn string   `json:"expireIn,omitempty"`
}

type CacheSpec struct {
	Key         string   `json:"key"`
	Paths       []string `json:"paths"`
	RestoreKeys []string `json:"restoreKeys,omitempty"`
	Policy      string   `json:"policy,omitempty"`
}

type SourceOrigin struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

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
	RawYAML       string           `json:"rawYaml"`
	ResolvedYAML  string           `json:"resolvedYaml,omitempty"`
	Jobs          []Job            `json:"jobs"`
	Graph         Graph            `json:"graph"`
	Validation    ValidationReport `json:"validation"`
	Features      []FeatureRef     `json:"featureRefs,omitempty"`
	Unsupported   []FeatureSupport `json:"unsupportedFeatures,omitempty"`
	ExecutionPlan *ExecutionPlan   `json:"executionPlan,omitempty"`
}

type Job struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Runner           string            `json:"runner,omitempty"`
	Stage            string            `json:"stage,omitempty"`
	Image            string            `json:"image,omitempty"`
	Needs            []string          `json:"needs"`
	If               string            `json:"if,omitempty"`
	Condition        *ConditionSpec    `json:"condition,omitempty"`
	Steps            []Step            `json:"steps"`
	Env              map[string]string `json:"env,omitempty"`
	Outputs          map[string]string `json:"outputs,omitempty"`
	Matrix           *MatrixSpec       `json:"matrix,omitempty"`
	Services         []ServiceSpec     `json:"services,omitempty"`
	Artifacts        []ArtifactSpec    `json:"artifacts,omitempty"`
	Caches           []CacheSpec       `json:"caches,omitempty"`
	AllowFailure     bool              `json:"allowFailure,omitempty"`
	When             string            `json:"when,omitempty"`
	Environment      string            `json:"environment,omitempty"`
	Origin           *SourceOrigin     `json:"origin,omitempty"`
	Support          SupportLevel      `json:"support"`
	Features         []FeatureRef      `json:"featureRefs,omitempty"`
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
	If               string            `json:"if,omitempty"`
	Condition        *ConditionSpec    `json:"condition,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	With             map[string]string `json:"with,omitempty"`
	ContinueOnError  bool              `json:"continueOnError,omitempty"`
	Origin           *SourceOrigin     `json:"origin,omitempty"`
	Support          SupportLevel      `json:"support"`
	Features         []FeatureRef      `json:"featureRefs,omitempty"`
	Unsupported      []FeatureSupport  `json:"unsupportedFeatures,omitempty"`
}

type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ID           string                 `json:"id"`
	Label        string                 `json:"label"`
	Support      SupportLevel           `json:"support"`
	LogicalJobID string                 `json:"logicalJobId,omitempty"`
	Matrix       map[string]interface{} `json:"matrix,omitempty"`
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
	FeatureID            string             `json:"featureId"`
	Feature              string             `json:"feature"`
	Provider             ProviderID         `json:"provider"`
	Category             string             `json:"category"`
	Path                 string             `json:"path,omitempty"`
	Origin               *SourceOrigin      `json:"origin,omitempty"`
	Support              SupportLevel       `json:"support"`
	RuntimeDisposition   RuntimeDisposition `json:"runtimeDisposition"`
	Message              string             `json:"message"`
	LocalBehavior        string             `json:"localBehavior"`
	HostedDifferences    string             `json:"hostedDifferences"`
	SecurityImplications string             `json:"securityImplications"`
	Fallback             string             `json:"fallback"`
	Documentation        string             `json:"documentation"`
}

type FeatureRef struct {
	ID     string        `json:"id"`
	Path   string        `json:"path,omitempty"`
	Origin *SourceOrigin `json:"origin,omitempty"`
}

type RuntimeDisposition string

const (
	RuntimeExecute     RuntimeDisposition = "execute"
	RuntimeEmulate     RuntimeDisposition = "emulate"
	RuntimeInspectOnly RuntimeDisposition = "inspect-only"
	RuntimeReject      RuntimeDisposition = "reject"
	RuntimeConsent     RuntimeDisposition = "consent"
)

type ValidationReport struct {
	Valid    bool              `json:"valid"`
	Support  SupportLevel      `json:"support"`
	Issues   []ValidationIssue `json:"issues"`
	Features []FeatureSupport  `json:"features"`
}

type RunRequest struct {
	RunID              string                                      `json:"-"`
	RepoPath           string                                      `json:"repoPath"`
	WorkflowPath       string                                      `json:"workflowPath"`
	Provider           ProviderID                                  `json:"provider"`
	JobID              string                                      `json:"jobId,omitempty"`
	EventName          string                                      `json:"eventName"`
	Inputs             map[string]string                           `json:"inputs"`
	Env                map[string]string                           `json:"env"`
	Secrets            map[string]string                           `json:"secrets"`
	BaseRef            string                                      `json:"baseRef,omitempty"`
	Concurrency        int                                         `json:"concurrency,omitempty"`
	MaxExpandedJobs    int                                         `json:"maxExpandedJobs,omitempty"`
	WorkspaceMode      string                                      `json:"workspaceMode,omitempty"`
	PreparedToken      string                                      `json:"preparedToken,omitempty"`
	NetworkAccess      string                                      `json:"networkAccess,omitempty"`
	MockOIDC           bool                                        `json:"mockOidc,omitempty"`
	MockOIDCClaims     map[string]string                           `json:"mockOidcClaims,omitempty"`
	WaitForApproval    func(context.Context, string) (bool, error) `json:"-"`
	AllowRemoteActions bool                                        `json:"-"`
	JobTimeoutSeconds  int                                         `json:"jobTimeoutSeconds,omitempty"`
	StepTimeoutSeconds int                                         `json:"stepTimeoutSeconds,omitempty"`
	MemoryMB           int64                                       `json:"memoryMb,omitempty"`
	CPUs               float64                                     `json:"cpus,omitempty"`
	PidsLimit          int64                                       `json:"pidsLimit,omitempty"`
}

type Settings struct {
	Concurrency        int     `json:"concurrency"`
	MaxExpandedJobs    int     `json:"maxExpandedJobs"`
	WorkspaceMode      string  `json:"workspaceMode"`
	NetworkAccess      string  `json:"networkAccess"`
	MockOIDC           bool    `json:"mockOidc"`
	JobTimeoutSeconds  int     `json:"jobTimeoutSeconds"`
	StepTimeoutSeconds int     `json:"stepTimeoutSeconds"`
	MemoryMB           int64   `json:"memoryMb"`
	CPUs               float64 `json:"cpus"`
	PidsLimit          int64   `json:"pidsLimit"`
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

type JobInstance struct {
	ID           string                 `json:"id"`
	LogicalJobID string                 `json:"logicalJobId"`
	Name         string                 `json:"name"`
	Needs        []string               `json:"needs"`
	Matrix       map[string]interface{} `json:"matrix,omitempty"`
	Job          Job                    `json:"job"`
}

type ExecutionPlan struct {
	Jobs             []JobInstance `json:"jobs"`
	MaxConcurrency   int           `json:"maxConcurrency"`
	MaxExpandedJobs  int           `json:"maxExpandedJobs"`
	RequiresConsent  bool          `json:"requiresConsent,omitempty"`
	RequiresApproval bool          `json:"requiresApproval,omitempty"`
}

type ArtifactRecord struct {
	ID          string    `json:"id"`
	RunID       string    `json:"runId"`
	JobID       string    `json:"jobId"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	SourcePaths []string  `json:"sourcePaths"`
	CreatedAt   time.Time `json:"createdAt"`
	Size        int64     `json:"size"`
}

type CacheRecord struct {
	ID         string    `json:"id"`
	Scope      string    `json:"scope"`
	Key        string    `json:"key"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

func CombineSupport(levels ...SupportLevel) SupportLevel {
	result := SupportSupportedLocal
	for _, level := range levels {
		if supportRank(level) > supportRank(result) {
			result = level
		}
	}
	return result
}

func ApplySourceFile(workflow *Workflow, file string) {
	if workflow == nil {
		return
	}
	setOriginFile := func(origin *SourceOrigin) {
		if origin != nil && origin.File == "" {
			origin.File = file
		}
	}
	for index := range workflow.Features {
		setOriginFile(workflow.Features[index].Origin)
	}
	for jobIndex := range workflow.Jobs {
		job := &workflow.Jobs[jobIndex]
		setOriginFile(job.Origin)
		for index := range job.Features {
			setOriginFile(job.Features[index].Origin)
		}
		for stepIndex := range job.Steps {
			step := &job.Steps[stepIndex]
			setOriginFile(step.Origin)
			for index := range step.Features {
				setOriginFile(step.Features[index].Origin)
			}
		}
	}
}

func supportRank(level SupportLevel) int {
	switch level {
	case SupportUnsupported:
		return 5
	case SupportRequiresConsent:
		return 4
	case SupportPartial:
		return 3
	case SupportValidationOnly:
		return 2
	case SupportEmulated:
		return 1
	default:
		return 0
	}
}
