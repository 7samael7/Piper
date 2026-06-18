package validation

import (
	"context"
	"fmt"

	"github.com/7samael7/Piper/engine/internal/expression"
	"github.com/7samael7/Piper/engine/internal/pipeline/graph"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/support"
)

func Validate(_ context.Context, workflow *model.Workflow) model.ValidationReport {
	registry, registryErr := support.Default()
	report := model.ValidationReport{
		Valid: true, Support: model.SupportSupportedLocal,
		Issues: []model.ValidationIssue{}, Features: []model.FeatureSupport{},
	}
	addIssue := func(severity model.IssueSeverity, code, message, path string, level model.SupportLevel) {
		report.Issues = append(report.Issues, model.ValidationIssue{
			Severity: severity, Code: code, Message: message, Path: path, Support: level,
		})
		if severity == model.SeverityError {
			report.Valid = false
		}
		report.Support = model.CombineSupport(report.Support, level)
	}
	if registryErr != nil {
		addIssue(model.SeverityError, "support.registry", registryErr.Error(), "", model.SupportUnsupported)
		return report
	}

	seen := map[string]bool{}
	resolveRefs := func(refs []model.FeatureRef) model.SupportLevel {
		level := model.SupportSupportedLocal
		for _, ref := range refs {
			key := ref.ID + "\x00" + ref.Path
			if seen[key] {
				if entry, ok := registry.Get(ref.ID); ok {
					level = model.CombineSupport(level, entry.Status)
				}
				continue
			}
			feature, err := registry.Resolve(ref)
			if err != nil {
				addIssue(model.SeverityError, "support.unknown_feature_id", err.Error(), ref.Path, model.SupportUnsupported)
				continue
			}
			seen[key] = true
			report.Features = append(report.Features, feature)
			level = model.CombineSupport(level, feature.Support)
			report.Support = model.CombineSupport(report.Support, feature.Support)
		}
		return level
	}

	base := []model.FeatureRef{
		support.Ref("common.workflow.discovery", workflow.Path, nil),
		support.Ref("common.graph", "jobs", nil),
		support.Ref("common.local-runner", "", nil),
	}
	resolveRefs(base)
	resolveRefs(workflow.Features)

	if len(workflow.Jobs) == 0 {
		addIssue(model.SeverityError, "workflow.no_jobs", "Workflow does not define any jobs.", "jobs", model.SupportUnsupported)
	}
	if _, err := graph.TopologicalSort(workflow); err != nil {
		addIssue(model.SeverityError, "workflow.graph", err.Error(), "jobs", model.SupportUnsupported)
	}

	for jobIndex := range workflow.Jobs {
		job := &workflow.Jobs[jobIndex]
		job.Support = resolveRefs(job.Features)
		jobPath := fmt.Sprintf("jobs.%s", job.ID)
		if workflow.Provider == model.ProviderGitHub && job.Runner == "" && job.ReusableWorkflow == "" {
			addIssue(model.SeverityWarning, "job.missing_runner", "Job does not declare runs-on.", jobPath+".runs-on", model.SupportPartial)
			job.Support = model.CombineSupport(job.Support, model.SupportPartial)
		}
		if job.Condition != nil {
			if err := expression.Validate(*job.Condition); err != nil {
				addIssue(model.SeverityError, err.Code, err.Message, jobPath+".if", model.SupportUnsupported)
				job.Support = model.CombineSupport(job.Support, model.SupportUnsupported)
			}
		}
		for stepIndex := range job.Steps {
			step := &job.Steps[stepIndex]
			step.Support = resolveRefs(step.Features)
			stepPath := fmt.Sprintf("%s.steps[%d]", jobPath, stepIndex)
			if step.Condition != nil {
				if err := expression.Validate(*step.Condition); err != nil {
					addIssue(model.SeverityError, err.Code, err.Message, stepPath+".if", model.SupportUnsupported)
					step.Support = model.CombineSupport(step.Support, model.SupportUnsupported)
				}
			}
			if step.Run == "" && step.Uses == "" {
				addIssue(model.SeverityWarning, "step.empty", "Step has neither a supported run command nor action/task.", stepPath, model.SupportUnsupported)
				step.Support = model.CombineSupport(step.Support, model.SupportUnsupported)
			}
			job.Support = model.CombineSupport(job.Support, step.Support)
		}
		report.Support = model.CombineSupport(report.Support, job.Support)
	}

	workflow.Support = report.Support
	return report
}
