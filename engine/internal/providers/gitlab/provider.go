package gitlab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/7samael7/Piper/engine/internal/pipeline/graph"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/pipeline/validation"
	"github.com/7samael7/Piper/engine/internal/providers/yamlutil"
	"gopkg.in/yaml.v3"
)

type Provider struct{}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) ID() model.ProviderID {
	return model.ProviderGitLab
}

func (p *Provider) Discover(ctx context.Context, repoPath string) ([]model.WorkflowSummary, error) {
	candidates := []string{".gitlab-ci.yml", ".gitlab-ci.yaml"}
	summaries := []model.WorkflowSummary{}
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		fullPath := filepath.Join(repoPath, candidate)
		if _, err := os.Stat(fullPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		workflow, _, err := p.Load(ctx, repoPath, candidate)
		if err != nil {
			summaries = append(summaries, model.WorkflowSummary{
				ID:       candidate,
				Provider: p.ID(),
				Name:     filepath.Base(candidate),
				Path:     candidate,
				Valid:    false,
				Support:  model.SupportUnsupported,
			})
			continue
		}
		summaries = append(summaries, workflow.WorkflowSummary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Path < summaries[j].Path
	})
	return summaries, nil
}

func (p *Provider) Load(ctx context.Context, repoPath, workflowPath string) (*model.Workflow, []byte, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	cleanPath := filepath.Clean(workflowPath)
	if filepath.IsAbs(cleanPath) {
		return nil, nil, fmt.Errorf("workflow path must be relative to the repository")
	}
	content, err := os.ReadFile(filepath.Join(repoPath, cleanPath))
	if err != nil {
		return nil, nil, err
	}
	workflow, err := parseWorkflow(filepath.ToSlash(cleanPath), content)
	if err != nil {
		return nil, content, err
	}
	workflow.Graph = graph.Build(workflow)
	workflow.Validation = p.Validate(ctx, workflow)
	workflow.Valid = workflow.Validation.Valid
	workflow.Support = workflow.Validation.Support
	return workflow, content, nil
}

func (p *Provider) Validate(ctx context.Context, workflow *model.Workflow) model.ValidationReport {
	return validation.Validate(ctx, workflow)
}

func parseWorkflow(path string, content []byte) (*model.Workflow, error) {
	root, err := yamlutil.RootMapping(content)
	if err != nil {
		return nil, err
	}

	stages := parseStages(root)
	jobs := parseJobs(root, stages)
	workflow := &model.Workflow{
		WorkflowSummary: model.WorkflowSummary{
			ID:       path,
			Provider: model.ProviderGitLab,
			Name:     "GitLab CI",
			Path:     path,
			Triggers: parseTriggers(root),
			JobCount: len(jobs),
			Valid:    true,
			Support:  model.SupportSupported,
		},
		RawYAML:     string(content),
		Jobs:        jobs,
		Unsupported: workflowUnsupported(root),
	}
	return workflow, nil
}

var gitlabReservedKeys = map[string]bool{
	"after_script":  true,
	"before_script": true,
	"cache":         true,
	"default":       true,
	"include":       true,
	"image":         true,
	"services":      true,
	"stages":        true,
	"types":         true,
	"variables":     true,
	"workflow":      true,
}

func parseTriggers(root *yaml.Node) []model.Trigger {
	triggers := []model.Trigger{
		{Name: "push"},
		{Name: "merge_request_event"},
		{Name: "web"},
		{Name: "schedule"},
	}
	if yamlutil.HasKey(root, "workflow") {
		triggers = append(triggers, model.Trigger{Name: "workflow:rules"})
	}
	return triggers
}

func parseStages(root *yaml.Node) []string {
	stages := yamlutil.StringSlice(yamlutil.MappingValue(root, "stages"))
	if len(stages) == 0 {
		stages = yamlutil.StringSlice(yamlutil.MappingValue(root, "types"))
	}
	if len(stages) == 0 {
		return []string{".pre", "build", "test", "deploy", ".post"}
	}
	return stages
}

type parsedJob struct {
	job           model.Job
	explicitNeeds bool
	stageIndex    int
}

func parseJobs(root *yaml.Node, stages []string) []model.Job {
	globalBefore := yamlutil.ScriptString(yamlutil.MappingValue(root, "before_script"))
	globalAfter := yamlutil.ScriptString(yamlutil.MappingValue(root, "after_script"))
	globalVariables := yamlutil.StringMap(yamlutil.MappingValue(root, "variables"))
	defaultNode := yamlutil.MappingValue(root, "default")
	defaultImage := yamlutil.ImageName(yamlutil.MappingValue(defaultNode, "image"))
	rootImage := yamlutil.ImageName(yamlutil.MappingValue(root, "image"))
	if rootImage == "" {
		rootImage = defaultImage
	}

	stageIndexes := map[string]int{}
	for index, stage := range stages {
		stageIndexes[stage] = index
	}

	parsed := []parsedJob{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		id := root.Content[i].Value
		body := root.Content[i+1]
		if gitlabReservedKeys[id] || strings.HasPrefix(id, ".") || body.Kind != yaml.MappingNode {
			continue
		}

		stage := yamlutil.ScalarString(yamlutil.MappingValue(body, "stage"))
		if stage == "" {
			stage = "test"
		}
		stageIndex, ok := stageIndexes[stage]
		if !ok {
			stageIndex = len(stages)
		}

		needs, explicitNeeds := parseNeeds(yamlutil.MappingValue(body, "needs"))
		image := yamlutil.ImageName(yamlutil.MappingValue(body, "image"))
		if image == "" {
			image = rootImage
		}
		runner := image
		if tags := yamlutil.StringSlice(yamlutil.MappingValue(body, "tags")); len(tags) > 0 {
			runner = "tags: " + strings.Join(tags, ", ")
			if image != "" {
				runner += " / image: " + image
			}
		}

		job := model.Job{
			ID:      id,
			Name:    id,
			Runner:  runner,
			Stage:   stage,
			Image:   image,
			Needs:   needs,
			If:      gitlabCondition(body),
			Env:     yamlutil.MergeStringMaps(globalVariables, yamlutil.StringMap(yamlutil.MappingValue(body, "variables"))),
			Steps:   gitlabSteps(globalBefore, yamlutil.ScriptString(yamlutil.MappingValue(body, "before_script")), yamlutil.ScriptString(yamlutil.MappingValue(body, "script")), yamlutil.ScriptString(yamlutil.MappingValue(body, "after_script")), globalAfter),
			Support: model.SupportSupported,
		}
		job.HasServices = yamlutil.HasKey(body, "services")
		job.HasStrategy = yamlutil.HasKey(body, "parallel")
		if trigger := yamlutil.ScalarString(yamlutil.MappingValue(body, "trigger")); trigger != "" {
			job.ReusableWorkflow = trigger
		} else if triggerNode := yamlutil.MappingValue(body, "trigger"); triggerNode != nil {
			job.ReusableWorkflow = yamlutil.YAMLToString(triggerNode)
		}
		job.Unsupported = gitlabJobUnsupported(id, body)
		if job.HasServices || job.HasStrategy || job.ReusableWorkflow != "" {
			job.Support = model.SupportUnsupported
		}
		for _, feature := range job.Unsupported {
			job.Support = model.CombineSupport(job.Support, feature.Support)
		}
		for _, step := range job.Steps {
			job.Support = model.CombineSupport(job.Support, step.Support)
		}

		parsed = append(parsed, parsedJob{job: job, explicitNeeds: explicitNeeds, stageIndex: stageIndex})
	}

	applyImplicitStageNeeds(parsed)
	jobs := make([]model.Job, 0, len(parsed))
	for _, item := range parsed {
		jobs = append(jobs, item.job)
	}
	return jobs
}

func parseNeeds(node *yaml.Node) ([]string, bool) {
	if node == nil {
		return nil, false
	}
	if node.Kind == yaml.SequenceNode {
		needs := []string{}
		for _, item := range node.Content {
			switch item.Kind {
			case yaml.ScalarNode:
				if item.Value != "" {
					needs = append(needs, item.Value)
				}
			case yaml.MappingNode:
				if job := yamlutil.ScalarString(yamlutil.MappingValue(item, "job")); job != "" {
					needs = append(needs, job)
				}
			}
		}
		return needs, true
	}
	return yamlutil.StringSlice(node), true
}

func applyImplicitStageNeeds(parsed []parsedJob) {
	jobsByStage := map[int][]string{}
	for _, item := range parsed {
		jobsByStage[item.stageIndex] = append(jobsByStage[item.stageIndex], item.job.ID)
	}
	for index := range parsed {
		if parsed[index].explicitNeeds || parsed[index].stageIndex <= 0 {
			continue
		}
		for previous := parsed[index].stageIndex - 1; previous >= 0; previous-- {
			if jobs := jobsByStage[previous]; len(jobs) > 0 {
				parsed[index].job.Needs = append(parsed[index].job.Needs, jobs...)
				break
			}
		}
	}
}

func gitlabSteps(globalBefore, jobBefore, script, jobAfter, globalAfter string) []model.Step {
	steps := []model.Step{}
	addRun := func(name, run string, support model.SupportLevel) {
		if strings.TrimSpace(run) == "" {
			return
		}
		steps = append(steps, model.Step{
			Name:    name,
			Run:     run,
			Support: support,
		})
	}
	addRun("before_script", globalBefore, model.SupportPartial)
	addRun("job before_script", jobBefore, model.SupportPartial)
	addRun(yamlutil.FirstLine(script), script, model.SupportSupported)
	addRun("job after_script", jobAfter, model.SupportPartial)
	addRun("after_script", globalAfter, model.SupportPartial)
	return steps
}

func gitlabCondition(body *yaml.Node) string {
	parts := []string{}
	for _, key := range []string{"rules", "only", "except"} {
		if node := yamlutil.MappingValue(body, key); node != nil {
			parts = append(parts, key+": "+yamlutil.YAMLToString(node))
		}
	}
	return strings.Join(parts, "\n")
}

func workflowUnsupported(root *yaml.Node) []model.FeatureSupport {
	features := []model.FeatureSupport{}
	if yamlutil.HasKey(root, "include") {
		features = append(features, unsupported("GitLab include", "include", model.SupportUnsupported, "GitLab include files are reported but not resolved by the MVP parser."))
	}
	if yamlutil.HasKey(root, "workflow") {
		features = append(features, unsupported("GitLab workflow rules", "workflow", model.SupportPartial, "Workflow rules are shown but not fully evaluated locally."))
	}
	if yamlutil.HasKey(root, "default") {
		features = append(features, unsupported("GitLab default", "default", model.SupportPartial, "Default image is applied, but other default keys are not fully expanded."))
	}
	return features
}

func gitlabJobUnsupported(id string, body *yaml.Node) []model.FeatureSupport {
	path := "jobs." + id
	features := []model.FeatureSupport{}
	if yamlutil.HasKey(body, "extends") {
		features = append(features, unsupported("GitLab extends", path+".extends", model.SupportUnsupported, "Job extends/templates are not resolved locally."))
	}
	if yamlutil.HasKey(body, "services") {
		features = append(features, unsupported("GitLab services", path+".services", model.SupportUnsupported, "Service containers are not started by the MVP executor."))
	}
	if yamlutil.HasKey(body, "parallel") {
		features = append(features, unsupported("GitLab parallel", path+".parallel", model.SupportUnsupported, "Parallel and matrix expansion are not implemented in the MVP."))
	}
	if yamlutil.HasKey(body, "trigger") {
		features = append(features, unsupported("GitLab child pipeline", path+".trigger", model.SupportUnsupported, "Child and multi-project pipelines are reported but not executed locally."))
	}
	for _, key := range []string{"artifacts", "cache", "dependencies", "environment", "resource_group", "coverage", "retry", "timeout"} {
		if yamlutil.HasKey(body, key) {
			features = append(features, unsupported("GitLab "+key, path+"."+key, model.SupportUnsupported, "This GitLab job feature is not emulated by the MVP executor."))
		}
	}
	if yamlutil.HasKey(body, "allow_failure") {
		features = append(features, unsupported("GitLab allow_failure", path+".allow_failure", model.SupportPartial, "allow_failure is displayed but does not change local run conclusions."))
	}
	return features
}

func unsupported(feature, path string, support model.SupportLevel, message string) model.FeatureSupport {
	return model.FeatureSupport{
		Feature: feature,
		Path:    path,
		Support: support,
		Message: message,
	}
}
