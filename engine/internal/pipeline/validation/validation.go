package validation

import (
	"context"
	"fmt"
	"strings"

	"github.com/7samael7/Piper/engine/internal/expression"
	"github.com/7samael7/Piper/engine/internal/pipeline/graph"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func Validate(_ context.Context, workflow *model.Workflow) model.ValidationReport {
	report := model.ValidationReport{
		Valid:    true,
		Support:  model.SupportSupported,
		Issues:   []model.ValidationIssue{},
		Features: []model.FeatureSupport{},
	}

	addIssue := func(severity model.IssueSeverity, code, message, path string, support model.SupportLevel) {
		report.Issues = append(report.Issues, model.ValidationIssue{
			Severity: severity,
			Code:     code,
			Message:  message,
			Path:     path,
			Support:  support,
		})
		if severity == model.SeverityError {
			report.Valid = false
		}
		report.Support = model.CombineSupport(report.Support, support)
	}
	addFeature := func(feature, path string, support model.SupportLevel, message string) {
		report.Features = append(report.Features, model.FeatureSupport{
			Feature: feature,
			Path:    path,
			Support: support,
			Message: message,
		})
		report.Support = model.CombineSupport(report.Support, support)
	}

	addFeature("workflow discovery", workflow.Path, model.SupportSupported, "Workflow was discovered from the local repository.")
	addFeature("job dependency graph", "jobs", model.SupportSupported, "Job dependencies are visualized from the needs graph.")
	addFeature("local runner parity", "", model.SupportPartial, "Local Docker execution does not perfectly match hosted CI runners.")
	for _, unsupported := range workflow.Unsupported {
		addFeature(unsupported.Feature, unsupported.Path, unsupported.Support, unsupported.Message)
	}

	if len(workflow.Jobs) == 0 {
		addIssue(model.SeverityError, "workflow.no_jobs", "Workflow does not define any jobs.", "jobs", model.SupportUnsupported)
	}

	for _, trigger := range workflow.Triggers {
		if workflow.Provider == model.ProviderGitHub && trigger.Name == "workflow_call" {
			addFeature("workflow_call", "on.workflow_call", model.SupportUnsupported, "Reusable workflow calls are not executed locally in the MVP.")
		}
	}

	if _, err := graph.TopologicalSort(workflow); err != nil {
		addIssue(model.SeverityError, "workflow.graph", err.Error(), "jobs", model.SupportUnsupported)
	}

	for _, job := range workflow.Jobs {
		jobPath := fmt.Sprintf("jobs.%s", job.ID)
		if workflow.Provider == model.ProviderGitHub && job.Runner == "" && job.ReusableWorkflow == "" {
			addIssue(model.SeverityWarning, "job.missing_runner", "Job does not declare runs-on.", jobPath+".runs-on", model.SupportPartial)
		}
		if job.Stage != "" {
			addFeature("stage ordering", jobPath+".stage", model.SupportPartial, "Stage ordering is approximated locally through dependency edges.")
		}
		if job.Image != "" {
			addFeature("job image", jobPath+".image", model.SupportPartial, "Job-specific Docker images are used locally when Docker can pull them.")
		}
		if job.ReusableWorkflow != "" {
			addFeature("reusable workflow job", jobPath+".uses", model.SupportUnsupported, "jobs.<id>.uses is reported but not executed locally.")
		}
		if job.HasContainer {
			addFeature("job container", jobPath+".container", model.SupportUnsupported, "Job container options are not emulated by the MVP executor.")
		}
		if job.HasServices {
			addFeature("service containers", jobPath+".services", model.SupportSupported, "Service containers run on an isolated per-job Docker network.")
		}
		if job.HasStrategy {
			if job.Matrix != nil {
				addFeature("strategy matrix", jobPath+".strategy", model.SupportSupported, "The matrix is expanded into deterministic local job instances.")
			} else {
				addFeature("job strategy", jobPath+".strategy", model.SupportUnsupported, "This strategy form cannot be expanded locally.")
			}
		}
		if job.If != "" {
			addFeature("job condition", jobPath+".if", model.SupportSupported, "The condition is evaluated before the job is scheduled.")
			if job.Condition != nil {
				if err := expression.Validate(*job.Condition); err != nil {
					addIssue(model.SeverityError, err.Code, err.Message, jobPath+".if", model.SupportUnsupported)
				}
			}
		}
		for _, unsupported := range job.Unsupported {
			addFeature(unsupported.Feature, unsupported.Path, unsupported.Support, unsupported.Message)
		}

		for index, step := range job.Steps {
			stepPath := fmt.Sprintf("%s.steps[%d]", jobPath, index)
			if step.Run != "" {
				addFeature("shell run step", stepPath+".run", model.SupportSupported, "Shell run steps execute inside a Docker container.")
				if containsExpression(step.Run) {
					addFeature("expressions in run step", stepPath+".run", model.SupportPartial, "Provider expression syntax inside scripts is not fully evaluated before execution.")
				}
			}
			if step.Condition != nil {
				addFeature("step condition", stepPath+".if", model.SupportSupported, "The condition is evaluated immediately before the step.")
				if err := expression.Validate(*step.Condition); err != nil {
					addIssue(model.SeverityError, err.Code, err.Message, stepPath+".if", model.SupportUnsupported)
				}
			}
			if step.Uses != "" {
				switch {
				case workflow.Provider == model.ProviderGitHub && strings.HasPrefix(step.Uses, "actions/checkout@"):
					addFeature("actions/checkout", stepPath+".uses", model.SupportPartial, "actions/checkout is treated as a local no-op because the repository is already mounted.")
				case workflow.Provider == model.ProviderGitHub && strings.HasPrefix(step.Uses, "actions/setup-dotnet@"):
					addFeature("actions/setup-dotnet", stepPath+".uses", model.SupportPartial, "actions/setup-dotnet is approximated with a matching .NET SDK Docker image; framework roll-forward may be used because hosted runner tool caches are not reproduced.")
				case workflow.Provider == model.ProviderGitHub && strings.HasPrefix(step.Uses, "actions/setup-node@"):
					addFeature("actions/setup-node", stepPath+".uses", model.SupportPartial, "actions/setup-node is approximated with a matching Node.js Docker image; caching and hosted-runner behavior are not emulated.")
				case workflow.Provider == model.ProviderAzure && step.Uses == "checkout":
					addFeature("Azure checkout", stepPath+".checkout", model.SupportPartial, "Azure checkout steps are treated as a local no-op because the repository is already mounted.")
				case workflow.Provider == model.ProviderGitHub && strings.HasPrefix(step.Uses, "./"):
					addFeature("local GitHub action", stepPath+".uses", model.SupportPartial, "Local JavaScript and composite actions execute when their declared runtime is available.")
				case workflow.Provider == model.ProviderGitHub && strings.Contains(step.Uses, "@"):
					addFeature("remote GitHub action", stepPath+".uses", model.SupportPartial, "Remote JavaScript and composite actions require explicit consent and are pinned to a resolved commit.")
				case workflow.Provider == model.ProviderAzure && isAzureBuiltinTask(step.Uses):
					addFeature("Azure task handler", stepPath+".task", model.SupportPartial, "This task uses a Piper local handler.")
				default:
					addFeature("external step", stepPath+".uses", model.SupportUnsupported, "No local handler is registered; execution fails visibly.")
				}
			}
			for _, unsupported := range step.Unsupported {
				addFeature(unsupported.Feature, unsupported.Path, unsupported.Support, unsupported.Message)
			}
			if step.Run == "" && step.Uses == "" {
				addIssue(model.SeverityWarning, "step.empty", "Step has neither run nor uses.", stepPath, model.SupportUnsupported)
			}
		}
	}

	return report
}

func isAzureBuiltinTask(value string) bool {
	value = strings.ToLower(value)
	for _, prefix := range []string{"publishbuildartifacts@", "downloadbuildartifacts@", "cache@"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func containsExpression(value string) bool {
	return strings.Contains(value, "${{") && strings.Contains(value, "}}")
}
