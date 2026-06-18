package github

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/7samael7/Piper/engine/internal/pipeline/graph"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/pipeline/plan"
	"github.com/7samael7/Piper/engine/internal/pipeline/validation"
	"github.com/7samael7/Piper/engine/internal/support"
	"github.com/7samael7/Piper/engine/internal/workspace"
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
	fullPath, err := workspace.Resolve(repoPath, cleanPath)
	if err != nil {
		return nil, nil, err
	}
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
	workflow.Graph = graph.Build(workflow)
	if workflow.Valid {
		executionPlan, compileErr := plan.Compile(workflow, plan.DefaultMaxExpandedJobs, plan.DefaultConcurrency)
		if compileErr != nil {
			return nil, content, compileErr
		}
		workflow.ExecutionPlan = executionPlan
		workflow.Graph = graph.BuildExecutionPlan(executionPlan)
		workflow.JobCount = len(executionPlan.Jobs)
	}
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

	jobs, err := parseJobs(mappingValue(root, "jobs"), parseStringMap(mappingValue(root, "env")))
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
		RawYAML:  string(content),
		Jobs:     jobs,
		Features: githubWorkflowFeatures(root),
	}
	model.ApplySourceFile(workflow, path)
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

func parseJobs(node *yaml.Node, workflowEnv map[string]string) ([]model.Job, error) {
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
		jobPath := "jobs." + id
		steps := parseSteps(mappingValue(body, "steps"), jobPath)
		applyRunDefaults(steps, mappingValue(body, "defaults"))
		job := model.Job{
			ID:          id,
			Name:        scalarString(mappingValue(body, "name")),
			Runner:      scalarOrListString(mappingValue(body, "runs-on")),
			Needs:       parseStringSlice(mappingValue(body, "needs")),
			If:          scalarString(mappingValue(body, "if")),
			Env:         mergeStringMaps(workflowEnv, parseStringMap(mappingValue(body, "env"))),
			Outputs:     parseStringMap(mappingValue(body, "outputs")),
			Environment: environmentName(mappingValue(body, "environment")),
			Steps:       steps,
			Support:     model.SupportSupported,
			Origin:      &model.SourceOrigin{Line: node.Content[i].Line, Column: node.Content[i].Column},
			Features:    []model.FeatureRef{support.Ref("github.metadata", jobPath, origin(node.Content[i]))},
		}
		if mappingValue(body, "runs-on") != nil {
			job.Features = append(job.Features, support.Ref("github.runner", jobPath+".runs-on", origin(mappingValue(body, "runs-on"))))
		}
		if mappingValue(body, "env") != nil || len(workflowEnv) > 0 {
			job.Features = append(job.Features, support.Ref("github.env", "jobs."+id+".env", origin(mappingValue(body, "env"))))
		}
		if mappingValue(body, "defaults") != nil {
			job.Features = append(job.Features, support.Ref("github.defaults", "jobs."+id+".defaults.run", origin(mappingValue(body, "defaults"))))
		}
		if mappingValue(body, "outputs") != nil {
			job.Features = append(job.Features, support.Ref("github.outputs", "jobs."+id+".outputs", origin(mappingValue(body, "outputs"))))
		}
		if mappingValue(body, "environment") != nil {
			job.Features = append(job.Features, support.Ref("github.environment", "jobs."+id+".environment", origin(mappingValue(body, "environment"))))
		}
		if mappingValue(body, "permissions") != nil || mappingValue(body, "concurrency") != nil {
			job.Features = append(job.Features, support.Ref("github.permissions-concurrency", "jobs."+id, origin(body)))
		}
		if job.If != "" {
			job.Condition = &model.ConditionSpec{Provider: model.ProviderGitHub, Original: job.If, Kind: "if"}
		}
		if job.Name == "" {
			job.Name = id
		}
		if uses := scalarString(mappingValue(body, "uses")); uses != "" {
			job.ReusableWorkflow = uses
			job.Support = model.SupportUnsupported
			job.Features = append(job.Features, support.Ref("github.reusable-workflow", "jobs."+id+".uses", origin(mappingValue(body, "uses"))))
		}
		if len(job.Steps) == 0 && job.ReusableWorkflow == "" {
			job.Features = append(job.Features, support.Ref("common.empty-job", jobPath, job.Origin))
			job.Support = model.SupportUnsupported
		}
		job.HasContainer = mappingValue(body, "container") != nil
		job.Services = parseServices(mappingValue(body, "services"))
		job.HasServices = len(job.Services) > 0
		job.Matrix, job.HasStrategy = parseStrategy(mappingValue(body, "strategy"))
		if job.HasContainer || (job.HasStrategy && job.Matrix == nil) {
			job.Support = model.SupportUnsupported
		}
		if job.HasContainer {
			job.Features = append(job.Features, support.Ref("github.job-container", "jobs."+id+".container", origin(mappingValue(body, "container"))))
		}
		if job.HasServices {
			job.Features = append(job.Features, support.Ref("github.services", "jobs."+id+".services", origin(mappingValue(body, "services"))))
		}
		if job.HasStrategy {
			featureID := "github.matrix"
			if job.Matrix == nil {
				featureID = "github.unknown"
			}
			job.Features = append(job.Features, support.Ref(featureID, "jobs."+id+".strategy", origin(mappingValue(body, "strategy"))))
		}
		if job.If != "" {
			job.Support = model.CombineSupport(job.Support, model.SupportPartial)
			job.Features = append(job.Features, support.Ref("github.conditions", "jobs."+id+".if", origin(mappingValue(body, "if"))))
		}
		job.Features = append(job.Features, unknownRefs(body, githubJobKeys, "jobs."+id, "github.unknown")...)
		for _, step := range job.Steps {
			job.Support = model.CombineSupport(job.Support, step.Support)
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func parseSteps(node *yaml.Node, jobPath string) []model.Step {
	if node == nil || node.Kind != yaml.SequenceNode {
		return []model.Step{}
	}

	steps := make([]model.Step, 0, len(node.Content))
	for i, item := range node.Content {
		stepPath := fmt.Sprintf("%s.steps[%d]", jobPath, i)
		if item.Kind != yaml.MappingNode {
			steps = append(steps, model.Step{
				Name:    fmt.Sprintf("Step %d", i+1),
				Support: model.SupportUnsupported,
				Origin:  origin(item),
				Features: []model.FeatureRef{
					support.Ref("common.empty-step", stepPath, origin(item)),
				},
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
			If:               scalarString(mappingValue(item, "if")),
			Env:              parseStringMap(mappingValue(item, "env")),
			With:             parseStringMap(mappingValue(item, "with")),
			ContinueOnError:  scalarBool(mappingValue(item, "continue-on-error")),
			Origin:           &model.SourceOrigin{Line: item.Line, Column: item.Column},
			Support:          model.SupportSupported,
		}
		ambiguous := executableKeyCount(item, "run", "uses") > 1
		if ambiguous {
			step.Features = append(step.Features, support.Ref("common.ambiguous-step", stepPath, origin(item)))
		}
		if step.Run != "" {
			step.Features = append(step.Features, support.Ref("common.shell", stepPath+".run", origin(mappingValue(item, "run"))))
			if containsExpression(step.Run) {
				step.Features = append(step.Features, support.Ref("github.interpolation", stepPath+".run", origin(mappingValue(item, "run"))))
			}
		}
		if mappingValue(item, "env") != nil {
			step.Features = append(step.Features, support.Ref("github.env", stepPath+".env", origin(mappingValue(item, "env"))))
		}
		if mappingValue(item, "shell") != nil || mappingValue(item, "working-directory") != nil {
			step.Features = append(step.Features, support.Ref("github.defaults", stepPath, origin(item)))
		}
		if step.ContinueOnError {
			step.Features = append(step.Features, support.Ref("github.continue-on-error", stepPath+".continue-on-error", origin(mappingValue(item, "continue-on-error"))))
		}
		if step.If != "" {
			step.Condition = &model.ConditionSpec{Provider: model.ProviderGitHub, Original: step.If, Kind: "if"}
			step.Features = append(step.Features, support.Ref("github.conditions", stepPath+".if", origin(mappingValue(item, "if"))))
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
			step.Features = append(step.Features, support.Ref("github.checkout", stepPath+".uses", origin(mappingValue(item, "uses"))))
		case strings.HasPrefix(step.Uses, "actions/setup-dotnet@"), strings.HasPrefix(step.Uses, "actions/setup-node@"):
			step.Support = model.SupportPartial
			step.Features = append(step.Features, support.Ref("github.setup-runtime", stepPath+".uses", origin(mappingValue(item, "uses"))))
		case strings.HasPrefix(step.Uses, "actions/upload-artifact@"), strings.HasPrefix(step.Uses, "actions/download-artifact@"), strings.HasPrefix(step.Uses, "actions/cache@"):
			step.Support = model.SupportPartial
			step.Features = append(step.Features, support.Ref("github.builtin-storage-action", stepPath+".uses", origin(mappingValue(item, "uses"))))
		case strings.HasPrefix(step.Uses, "./"):
			step.Support = model.SupportPartial
			step.Features = append(step.Features, support.Ref("github.local-action", stepPath+".uses", origin(mappingValue(item, "uses"))))
		case strings.Contains(step.Uses, "@"):
			step.Support = model.SupportPartial
			step.Features = append(step.Features, support.Ref("github.remote-action", stepPath+".uses", origin(mappingValue(item, "uses"))))
		case step.Uses != "":
			step.Support = model.SupportUnsupported
			step.Features = append(step.Features, support.Ref("github.unsupported-action", stepPath+".uses", origin(mappingValue(item, "uses"))))
		default:
			step.Support = model.SupportUnsupported
			step.Features = append(step.Features, support.Ref("common.empty-step", stepPath, origin(item)))
		}
		if ambiguous {
			step.Support = model.SupportUnsupported
		}
		step.Features = append(step.Features, unknownRefs(item, githubStepKeys, stepPath, "github.unknown")...)
		steps = append(steps, step)
	}
	return steps
}

func executableKeyCount(node *yaml.Node, keys ...string) int {
	count := 0
	for _, key := range keys {
		if mappingValue(node, key) != nil {
			count++
		}
	}
	return count
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

func parseStrategy(node *yaml.Node) (*model.MatrixSpec, bool) {
	if node == nil {
		return nil, false
	}
	matrixNode := mappingValue(node, "matrix")
	if matrixNode == nil || matrixNode.Kind != yaml.MappingNode {
		return nil, true
	}
	spec := &model.MatrixSpec{
		Dimensions: map[string][]interface{}{},
		FailFast:   true,
	}
	if failFast := mappingValue(node, "fail-fast"); failFast != nil {
		spec.FailFast = scalarBool(failFast)
	}
	if maxParallel := mappingValue(node, "max-parallel"); maxParallel != nil {
		fmt.Sscanf(scalarString(maxParallel), "%d", &spec.MaxParallel)
	}
	for i := 0; i+1 < len(matrixNode.Content); i += 2 {
		key := matrixNode.Content[i].Value
		value := matrixNode.Content[i+1]
		switch key {
		case "include":
			spec.Include = parseObjectList(value)
		case "exclude":
			spec.Exclude = parseObjectList(value)
		default:
			if value.Kind == yaml.SequenceNode {
				items := make([]interface{}, 0, len(value.Content))
				for _, item := range value.Content {
					items = append(items, yamlValue(item))
				}
				spec.Dimensions[key] = items
			}
		}
	}
	return spec, true
}

func parseServices(node *yaml.Node) []model.ServiceSpec {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	services := make([]model.ServiceSpec, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		body := node.Content[i+1]
		service := model.ServiceSpec{Name: name, Image: scalarString(body), Aliases: []string{name}, StartupTimeout: 60}
		if body.Kind == yaml.MappingNode {
			service.Image = scalarString(mappingValue(body, "image"))
			service.Env = parseStringMap(mappingValue(body, "env"))
			service.Ports = parseStringSlice(mappingValue(body, "ports"))
			service.Options = scalarString(mappingValue(body, "options"))
		}
		if service.Image != "" {
			services = append(services, service)
		}
	}
	return services
}

func parseObjectList(node *yaml.Node) []map[string]interface{} {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		values := map[string]interface{}{}
		for i := 0; i+1 < len(item.Content); i += 2 {
			values[item.Content[i].Value] = yamlValue(item.Content[i+1])
		}
		result = append(result, values)
	}
	return result
}

func yamlValue(node *yaml.Node) interface{} {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.ScalarNode {
		return scalarString(node)
	}
	switch node.Tag {
	case "!!bool":
		return scalarBool(node)
	case "!!int":
		var value int
		if _, err := fmt.Sscanf(node.Value, "%d", &value); err == nil {
			return value
		}
	}
	return node.Value
}

func mergeStringMaps(values ...map[string]string) map[string]string {
	var result map[string]string
	for _, entries := range values {
		for key, value := range entries {
			if result == nil {
				result = map[string]string{}
			}
			result[key] = value
		}
	}
	return result
}

func environmentName(node *yaml.Node) string {
	if node != nil && node.Kind == yaml.MappingNode {
		return scalarString(mappingValue(node, "name"))
	}
	return scalarString(node)
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

var githubWorkflowKeys = map[string]bool{
	"name": true, "run-name": true, "on": true, "permissions": true, "env": true,
	"concurrency": true, "jobs": true,
}

var githubJobKeys = map[string]bool{
	"name": true, "runs-on": true, "needs": true, "if": true, "env": true,
	"outputs": true, "environment": true, "steps": true, "uses": true, "with": true,
	"secrets": true, "container": true, "services": true, "strategy": true,
	"defaults": true, "permissions": true, "concurrency": true,
}

var githubStepKeys = map[string]bool{
	"id": true, "name": true, "uses": true, "run": true, "shell": true,
	"working-directory": true, "if": true, "env": true, "with": true,
	"continue-on-error": true,
}

func githubWorkflowFeatures(root *yaml.Node) []model.FeatureRef {
	features := []model.FeatureRef{support.Ref("github.metadata", "workflow", origin(root))}
	if mappingValue(root, "on") != nil {
		features = append(features, support.Ref("github.triggers", "on", origin(mappingValue(root, "on"))))
	}
	if mappingValue(root, "env") != nil {
		features = append(features, support.Ref("github.env", "env", origin(mappingValue(root, "env"))))
	}
	if mappingValue(root, "permissions") != nil || mappingValue(root, "concurrency") != nil {
		features = append(features, support.Ref("github.permissions-concurrency", "workflow", origin(root)))
	}
	for _, trigger := range parseTriggers(mappingValue(root, "on")) {
		if trigger.Name == "workflow_call" {
			features = append(features, support.Ref("github.reusable-workflow", "on.workflow_call", origin(mappingValue(root, "on"))))
		}
	}
	return append(features, unknownRefs(root, githubWorkflowKeys, "", "github.unknown")...)
}

func unknownRefs(node *yaml.Node, known map[string]bool, prefix, featureID string) []model.FeatureRef {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	result := []model.FeatureRef{}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index]
		if known[key.Value] {
			continue
		}
		path := key.Value
		if prefix != "" {
			path = prefix + "." + key.Value
		}
		result = append(result, support.Ref(featureID, path, origin(key)))
	}
	return result
}

func origin(node *yaml.Node) *model.SourceOrigin {
	if node == nil {
		return nil
	}
	return &model.SourceOrigin{Line: node.Line, Column: node.Column}
}

func containsExpression(value string) bool {
	return strings.Contains(value, "${{") && strings.Contains(value, "}}")
}
