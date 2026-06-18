package github

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pipeline-workbench/engine/internal/pipeline/graph"
	"github.com/pipeline-workbench/engine/internal/pipeline/model"
	"github.com/pipeline-workbench/engine/internal/pipeline/validation"
	"gopkg.in/yaml.v3"
)

type Provider struct{}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) ID() model.ProviderID {
	return model.ProviderGitHub
}

func (p *Provider) Discover(ctx context.Context, repoPath string) ([]model.WorkflowSummary, error) {
	workflowsDir := filepath.Join(repoPath, ".github", "workflows")
	entries := []string{}
	if _, err := os.Stat(workflowsDir); err != nil {
		if os.IsNotExist(err) {
			return []model.WorkflowSummary{}, nil
		}
		return nil, err
	}

	if err := filepath.WalkDir(workflowsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yml" || ext == ".yaml" {
			rel, relErr := filepath.Rel(repoPath, path)
			if relErr != nil {
				return relErr
			}
			entries = append(entries, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		return nil, err
	}

	sort.Strings(entries)
	summaries := make([]model.WorkflowSummary, 0, len(entries))
	for _, rel := range entries {
		workflow, _, err := p.Load(ctx, repoPath, rel)
		if err != nil {
			summaries = append(summaries, model.WorkflowSummary{
				ID:       rel,
				Provider: p.ID(),
				Name:     filepath.Base(rel),
				Path:     rel,
				Valid:    false,
				Support:  model.SupportUnsupported,
			})
			continue
		}
		summaries = append(summaries, workflow.WorkflowSummary)
	}

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
	fullPath := filepath.Join(repoPath, cleanPath)
	content, err := os.ReadFile(fullPath)
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
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("workflow root must be a YAML mapping")
	}

	root := document.Content[0]
	name := scalarString(mappingValue(root, "name"))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	jobs, err := parseJobs(mappingValue(root, "jobs"))
	if err != nil {
		return nil, err
	}

	workflow := &model.Workflow{
		WorkflowSummary: model.WorkflowSummary{
			ID:       path,
			Provider: model.ProviderGitHub,
			Name:     name,
			Path:     path,
			Triggers: parseTriggers(mappingValue(root, "on")),
			JobCount: len(jobs),
			Valid:    true,
			Support:  model.SupportSupported,
		},
		RawYAML: string(content),
		Jobs:    jobs,
	}
	return workflow, nil
}

func parseTriggers(node *yaml.Node) []model.Trigger {
	if node == nil {
		return []model.Trigger{}
	}

	switch node.Kind {
	case yaml.ScalarNode:
		return []model.Trigger{{Name: node.Value}}
	case yaml.SequenceNode:
		triggers := make([]model.Trigger, 0, len(node.Content))
		for _, item := range node.Content {
			name := scalarString(item)
			if name != "" {
				triggers = append(triggers, model.Trigger{Name: name})
			}
		}
		return triggers
	case yaml.MappingNode:
		triggers := make([]model.Trigger, 0, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			name := node.Content[i].Value
			trigger := model.Trigger{Name: name}
			if name == "workflow_dispatch" {
				trigger.Inputs = parseWorkflowDispatchInputs(node.Content[i+1])
			}
			triggers = append(triggers, trigger)
		}
		return triggers
	default:
		return []model.Trigger{}
	}
}

func parseWorkflowDispatchInputs(node *yaml.Node) []model.TriggerInput {
	inputsNode := mappingValue(node, "inputs")
	if inputsNode == nil || inputsNode.Kind != yaml.MappingNode {
		return nil
	}

	inputs := make([]model.TriggerInput, 0, len(inputsNode.Content)/2)
	for i := 0; i+1 < len(inputsNode.Content); i += 2 {
		name := inputsNode.Content[i].Value
		body := inputsNode.Content[i+1]
		inputs = append(inputs, model.TriggerInput{
			Name:        name,
			Description: scalarString(mappingValue(body, "description")),
			Required:    scalarBool(mappingValue(body, "required")),
			Default:     scalarString(mappingValue(body, "default")),
			Type:        scalarString(mappingValue(body, "type")),
		})
	}
	return inputs
}

func parseJobs(node *yaml.Node) ([]model.Job, error) {
	if node == nil {
		return []model.Job{}, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("jobs must be a YAML mapping")
	}

	jobs := make([]model.Job, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		id := node.Content[i].Value
		body := node.Content[i+1]
		if body.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("jobs.%s must be a YAML mapping", id)
		}
		steps := parseSteps(mappingValue(body, "steps"))
		applyRunDefaults(steps, mappingValue(body, "defaults"))
		job := model.Job{
			ID:      id,
			Name:    scalarString(mappingValue(body, "name")),
			Runner:  scalarOrListString(mappingValue(body, "runs-on")),
			Needs:   parseStringSlice(mappingValue(body, "needs")),
			If:      scalarString(mappingValue(body, "if")),
			Env:     parseStringMap(mappingValue(body, "env")),
			Steps:   steps,
			Support: model.SupportSupported,
		}
		if job.Name == "" {
			job.Name = id
		}
		if uses := scalarString(mappingValue(body, "uses")); uses != "" {
			job.ReusableWorkflow = uses
			job.Support = model.SupportUnsupported
		}
		job.HasContainer = mappingValue(body, "container") != nil
		job.HasServices = mappingValue(body, "services") != nil
		job.HasStrategy = mappingValue(body, "strategy") != nil
		if job.HasContainer || job.HasServices || job.HasStrategy {
			job.Support = model.SupportUnsupported
		}
		if job.If != "" {
			job.Support = model.CombineSupport(job.Support, model.SupportPartial)
		}
		for _, step := range job.Steps {
			job.Support = model.CombineSupport(job.Support, step.Support)
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func parseSteps(node *yaml.Node) []model.Step {
	if node == nil || node.Kind != yaml.SequenceNode {
		return []model.Step{}
	}

	steps := make([]model.Step, 0, len(node.Content))
	for i, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			steps = append(steps, model.Step{
				Name:    fmt.Sprintf("Step %d", i+1),
				Support: model.SupportUnsupported,
			})
			continue
		}
		step := model.Step{
			ID:               scalarString(mappingValue(item, "id")),
			Name:             scalarString(mappingValue(item, "name")),
			Uses:             scalarString(mappingValue(item, "uses")),
			Run:              scalarString(mappingValue(item, "run")),
			Shell:            scalarString(mappingValue(item, "shell")),
			WorkingDirectory: scalarString(mappingValue(item, "working-directory")),
			Env:              parseStringMap(mappingValue(item, "env")),
			With:             parseStringMap(mappingValue(item, "with")),
			Support:          model.SupportSupported,
		}
		if step.Name == "" {
			switch {
			case step.Uses != "":
				step.Name = step.Uses
			case step.Run != "":
				step.Name = firstLine(step.Run)
			default:
				step.Name = fmt.Sprintf("Step %d", i+1)
			}
		}
		switch {
		case step.Run != "":
			step.Support = model.SupportSupported
		case strings.HasPrefix(step.Uses, "actions/checkout@"):
			step.Support = model.SupportPartial
		case strings.HasPrefix(step.Uses, "actions/setup-dotnet@"), strings.HasPrefix(step.Uses, "actions/setup-node@"):
			step.Support = model.SupportPartial
		case step.Uses != "":
			step.Support = model.SupportUnsupported
		default:
			step.Support = model.SupportUnsupported
		}
		steps = append(steps, step)
	}
	return steps
}

func applyRunDefaults(steps []model.Step, defaults *yaml.Node) {
	runDefaults := mappingValue(defaults, "run")
	if runDefaults == nil {
		return
	}
	workingDirectory := scalarString(mappingValue(runDefaults, "working-directory"))
	shell := scalarString(mappingValue(runDefaults, "shell"))
	for index := range steps {
		if steps[index].Run == "" {
			continue
		}
		if steps[index].WorkingDirectory == "" {
			steps[index].WorkingDirectory = workingDirectory
		}
		if steps[index].Shell == "" {
			steps[index].Shell = shell
		}
	}
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func scalarString(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	return yamlToString(node)
}

func scalarOrListString(node *yaml.Node) string {
	switch {
	case node == nil:
		return ""
	case node.Kind == yaml.SequenceNode:
		values := parseStringSlice(node)
		return strings.Join(values, ", ")
	default:
		return scalarString(node)
	}
}

func scalarBool(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.ScalarNode {
		return false
	}
	return strings.EqualFold(node.Value, "true") || node.Value == "yes"
}

func parseStringSlice(node *yaml.Node) []string {
	if node == nil {
		return []string{}
	}
	if node.Kind == yaml.SequenceNode {
		values := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			value := scalarString(item)
			if value != "" {
				values = append(values, value)
			}
		}
		return values
	}
	value := scalarString(node)
	if value == "" {
		return []string{}
	}
	return []string{value}
}

func parseStringMap(node *yaml.Node) map[string]string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	values := make(map[string]string, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		values[node.Content[i].Value] = scalarString(node.Content[i+1])
	}
	return values
}

func yamlToString(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	bytes, err := yaml.Marshal(node)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes))
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "run"
	}
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	if len(value) > 64 {
		return value[:61] + "..."
	}
	return value
}
