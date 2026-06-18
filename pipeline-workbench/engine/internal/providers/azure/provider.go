package azure

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
	"github.com/pipeline-workbench/engine/internal/providers/yamlutil"
	"gopkg.in/yaml.v3"
)

type Provider struct{}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) ID() model.ProviderID {
	return model.ProviderAzure
}

func (p *Provider) Discover(ctx context.Context, repoPath string) ([]model.WorkflowSummary, error) {
	entries, err := discoverFiles(ctx, repoPath)
	if err != nil {
		return nil, err
	}
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

func discoverFiles(ctx context.Context, repoPath string) ([]string, error) {
	seen := map[string]bool{}
	entries := []string{}
	add := func(rel string) {
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			seen[rel] = true
			entries = append(entries, rel)
		}
	}

	for _, rel := range []string{"azure-pipelines.yml", "azure-pipelines.yaml"} {
		if _, err := os.Stat(filepath.Join(repoPath, rel)); err == nil {
			add(rel)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	for _, dir := range []string{".azure-pipelines", "azure-pipelines", "pipelines"} {
		fullDir := filepath.Join(repoPath, dir)
		if _, err := os.Stat(fullDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := filepath.WalkDir(fullDir, func(path string, entry fs.DirEntry, err error) error {
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
				add(rel)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}

	sort.Strings(entries)
	return entries, nil
}

func parseWorkflow(path string, content []byte) (*model.Workflow, error) {
	root, err := yamlutil.RootMapping(content)
	if err != nil {
		return nil, err
	}
	name := yamlutil.ScalarString(yamlutil.MappingValue(root, "name"))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	jobs := parseWorkflowJobs(root)
	workflow := &model.Workflow{
		WorkflowSummary: model.WorkflowSummary{
			ID:       path,
			Provider: model.ProviderAzure,
			Name:     name,
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

func parseTriggers(root *yaml.Node) []model.Trigger {
	triggers := []model.Trigger{}
	if yamlutil.HasKey(root, "trigger") {
		triggers = append(triggers, model.Trigger{Name: "trigger"})
	}
	if yamlutil.HasKey(root, "pr") {
		triggers = append(triggers, model.Trigger{Name: "pr"})
	}
	if yamlutil.HasKey(root, "schedules") {
		triggers = append(triggers, model.Trigger{Name: "schedules"})
	}
	if len(triggers) == 0 {
		triggers = append(triggers, model.Trigger{Name: "manual"})
	}
	return triggers
}

type parsedJob struct {
	job            model.Job
	rawNeeds       []string
	explicitNeeds  bool
	stageID        string
	stageIndex     int
	stageDependsOn []string
}

func parseWorkflowJobs(root *yaml.Node) []model.Job {
	rootVariables := parseVariables(yamlutil.MappingValue(root, "variables"))
	rootPool := parsePool(yamlutil.MappingValue(root, "pool"))
	if stagesNode := yamlutil.MappingValue(root, "stages"); stagesNode != nil && stagesNode.Kind == yaml.SequenceNode {
		return parseStageJobs(stagesNode, rootPool, rootVariables)
	}
	if jobsNode := yamlutil.MappingValue(root, "jobs"); jobsNode != nil {
		parsed := parseJobsNode(jobsNode, "", 0, nil, rootPool, rootVariables)
		return finalizeJobs(parsed)
	}
	if stepsNode := yamlutil.MappingValue(root, "steps"); stepsNode != nil {
		job := model.Job{
			ID:      "pipeline",
			Name:    "Pipeline",
			Runner:  rootPool,
			Needs:   []string{},
			Env:     rootVariables,
			Steps:   parseSteps(stepsNode, "jobs.pipeline"),
			Support: model.SupportSupported,
		}
		for _, step := range job.Steps {
			job.Support = model.CombineSupport(job.Support, step.Support)
		}
		return []model.Job{job}
	}
	return []model.Job{}
}

func parseStageJobs(stagesNode *yaml.Node, rootPool string, rootVariables map[string]string) []model.Job {
	parsed := []parsedJob{}
	stageIndex := 0
	for _, item := range stagesNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		stageID := yamlutil.ScalarString(yamlutil.MappingValue(item, "stage"))
		if stageID == "" {
			stageID = fmt.Sprintf("stage_%d", stageIndex+1)
		}
		stagePool := parsePool(yamlutil.MappingValue(item, "pool"))
		if stagePool == "" {
			stagePool = rootPool
		}
		stageVariables := yamlutil.MergeStringMaps(rootVariables, parseVariables(yamlutil.MappingValue(item, "variables")))
		stageDependsOn, _ := parseDependsOn(yamlutil.MappingValue(item, "dependsOn"))
		stageJobs := parseJobsNode(yamlutil.MappingValue(item, "jobs"), stageID, stageIndex, stageDependsOn, stagePool, stageVariables)
		parsed = append(parsed, stageJobs...)
		stageIndex++
	}
	return finalizeJobs(parsed)
}

func parseJobsNode(node *yaml.Node, stageID string, stageIndex int, stageDependsOn []string, inheritedPool string, inheritedVariables map[string]string) []parsedJob {
	if node == nil {
		return nil
	}
	items := []*yaml.Node{}
	if node.Kind == yaml.SequenceNode {
		items = node.Content
	} else if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			wrapper := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "job"},
				node.Content[i],
				{Kind: yaml.ScalarNode, Value: "steps"},
				yamlutil.MappingValue(node.Content[i+1], "steps"),
			}}
			items = append(items, wrapper)
		}
	}

	parsed := []parsedJob{}
	for index, item := range items {
		if item == nil || item.Kind != yaml.MappingNode {
			continue
		}
		jobID := yamlutil.ScalarString(yamlutil.MappingValue(item, "job"))
		isDeployment := false
		if jobID == "" {
			jobID = yamlutil.ScalarString(yamlutil.MappingValue(item, "deployment"))
			isDeployment = jobID != ""
		}
		if jobID == "" {
			jobID = fmt.Sprintf("job_%d", index+1)
		}
		fullID := jobID
		if stageID != "" {
			fullID = stageID + "." + jobID
		}
		pool := parsePool(yamlutil.MappingValue(item, "pool"))
		if pool == "" {
			pool = inheritedPool
		}
		rawNeeds, explicitNeeds := parseDependsOn(yamlutil.MappingValue(item, "dependsOn"))
		job := model.Job{
			ID:      fullID,
			Name:    firstNonEmpty(yamlutil.ScalarString(yamlutil.MappingValue(item, "displayName")), jobID),
			Runner:  pool,
			Stage:   stageID,
			Needs:   []string{},
			If:      yamlutil.ScalarString(yamlutil.MappingValue(item, "condition")),
			Env:     yamlutil.MergeStringMaps(inheritedVariables, parseVariables(yamlutil.MappingValue(item, "variables"))),
			Steps:   parseAzureJobSteps(item, fullID, isDeployment),
			Support: model.SupportSupported,
		}
		job.HasContainer = yamlutil.HasKey(item, "container")
		job.HasServices = yamlutil.HasKey(item, "services")
		job.HasStrategy = yamlutil.HasKey(item, "strategy")
		job.Unsupported = jobUnsupported(fullID, item, isDeployment)
		if job.HasContainer || job.HasServices || job.HasStrategy || isDeployment {
			job.Support = model.SupportUnsupported
		}
		if job.If != "" {
			job.Support = model.CombineSupport(job.Support, model.SupportPartial)
		}
		for _, feature := range job.Unsupported {
			job.Support = model.CombineSupport(job.Support, feature.Support)
		}
		for _, step := range job.Steps {
			job.Support = model.CombineSupport(job.Support, step.Support)
		}
		parsed = append(parsed, parsedJob{
			job:            job,
			rawNeeds:       rawNeeds,
			explicitNeeds:  explicitNeeds,
			stageID:        stageID,
			stageIndex:     stageIndex,
			stageDependsOn: stageDependsOn,
		})
	}
	return parsed
}

func parseAzureJobSteps(item *yaml.Node, fullID string, isDeployment bool) []model.Step {
	if !isDeployment {
		return parseSteps(yamlutil.MappingValue(item, "steps"), "jobs."+fullID)
	}
	strategy := yamlutil.MappingValue(item, "strategy")
	runOnce := yamlutil.MappingValue(strategy, "runOnce")
	deploy := yamlutil.MappingValue(runOnce, "deploy")
	return parseSteps(yamlutil.MappingValue(deploy, "steps"), "jobs."+fullID+".strategy.runOnce.deploy")
}

func finalizeJobs(parsed []parsedJob) []model.Job {
	stageJobs := map[string][]string{}
	stageIndexJobs := map[int][]string{}
	jobIDs := map[string]bool{}
	for _, item := range parsed {
		jobIDs[item.job.ID] = true
		if item.stageID != "" {
			stageJobs[item.stageID] = append(stageJobs[item.stageID], item.job.ID)
			stageIndexJobs[item.stageIndex] = append(stageIndexJobs[item.stageIndex], item.job.ID)
		}
	}

	for index := range parsed {
		if len(parsed[index].rawNeeds) > 0 || parsed[index].explicitNeeds {
			parsed[index].job.Needs = resolveNeeds(parsed[index].rawNeeds, parsed[index].stageID, jobIDs)
			continue
		}
		if len(parsed[index].stageDependsOn) > 0 {
			parsed[index].job.Needs = stageDependencyJobs(parsed[index].stageDependsOn, stageJobs)
			continue
		}
		if parsed[index].stageID != "" && parsed[index].stageIndex > 0 {
			for previous := parsed[index].stageIndex - 1; previous >= 0; previous-- {
				if jobs := stageIndexJobs[previous]; len(jobs) > 0 {
					parsed[index].job.Needs = append(parsed[index].job.Needs, jobs...)
					break
				}
			}
		}
	}

	jobs := make([]model.Job, 0, len(parsed))
	for _, item := range parsed {
		jobs = append(jobs, item.job)
	}
	return jobs
}

func resolveNeeds(raw []string, stageID string, jobIDs map[string]bool) []string {
	needs := []string{}
	for _, need := range raw {
		if need == "" {
			continue
		}
		candidate := need
		if stageID != "" && !strings.Contains(need, ".") {
			stageCandidate := stageID + "." + need
			if jobIDs[stageCandidate] {
				candidate = stageCandidate
			}
		}
		needs = append(needs, candidate)
	}
	return needs
}

func stageDependencyJobs(stageDependsOn []string, stageJobs map[string][]string) []string {
	needs := []string{}
	for _, stage := range stageDependsOn {
		needs = append(needs, stageJobs[stage]...)
	}
	return needs
}

func parseSteps(node *yaml.Node, path string) []model.Step {
	if node == nil || node.Kind != yaml.SequenceNode {
		return []model.Step{}
	}
	steps := make([]model.Step, 0, len(node.Content))
	for index, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			steps = append(steps, model.Step{Name: fmt.Sprintf("Step %d", index+1), Support: model.SupportUnsupported})
			continue
		}
		stepPath := fmt.Sprintf("%s.steps[%d]", path, index)
		step := model.Step{
			Name:             firstNonEmpty(yamlutil.ScalarString(yamlutil.MappingValue(item, "displayName")), fmt.Sprintf("Step %d", index+1)),
			Env:              yamlutil.StringMap(yamlutil.MappingValue(item, "env")),
			WorkingDirectory: yamlutil.ScalarString(yamlutil.MappingValue(item, "workingDirectory")),
			Support:          model.SupportSupported,
		}
		switch {
		case yamlutil.HasKey(item, "bash"):
			step.Run = yamlutil.ScriptString(yamlutil.MappingValue(item, "bash"))
			step.Shell = "bash"
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = yamlutil.FirstLine(step.Run)
			}
		case yamlutil.HasKey(item, "script"):
			step.Run = yamlutil.ScriptString(yamlutil.MappingValue(item, "script"))
			step.Shell = "bash"
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = yamlutil.FirstLine(step.Run)
			}
		case yamlutil.HasKey(item, "checkout"):
			step.Uses = "checkout"
			step.Support = model.SupportPartial
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = "checkout " + yamlutil.ScalarString(yamlutil.MappingValue(item, "checkout"))
			}
		case yamlutil.HasKey(item, "task"):
			step.Uses = yamlutil.ScalarString(yamlutil.MappingValue(item, "task"))
			step.Support = model.SupportUnsupported
			step.Unsupported = append(step.Unsupported, feature("Azure task", stepPath+".task", model.SupportUnsupported, "Azure task execution is not implemented in the MVP."))
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = step.Uses
			}
		case yamlutil.HasKey(item, "template"):
			step.Uses = yamlutil.ScalarString(yamlutil.MappingValue(item, "template"))
			step.Support = model.SupportUnsupported
			step.Unsupported = append(step.Unsupported, feature("Azure step template", stepPath+".template", model.SupportUnsupported, "Azure step templates are reported but not expanded locally."))
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = step.Uses
			}
		case yamlutil.HasKey(item, "pwsh"), yamlutil.HasKey(item, "powershell"):
			step.Uses = "powershell"
			step.Support = model.SupportUnsupported
			step.Unsupported = append(step.Unsupported, feature("Azure PowerShell step", stepPath, model.SupportUnsupported, "PowerShell steps are reported but not executed by the MVP bash executor."))
		default:
			step.Support = model.SupportUnsupported
			step.Unsupported = append(step.Unsupported, feature("Azure step", stepPath, model.SupportUnsupported, "This Azure step type is not implemented in the MVP."))
		}
		steps = append(steps, step)
	}
	return steps
}

func parseDependsOn(node *yaml.Node) ([]string, bool) {
	if node == nil {
		return nil, false
	}
	return yamlutil.StringSlice(node), true
}

func parsePool(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.MappingNode {
		if vmImage := yamlutil.ScalarString(yamlutil.MappingValue(node, "vmImage")); vmImage != "" {
			return vmImage
		}
		if name := yamlutil.ScalarString(yamlutil.MappingValue(node, "name")); name != "" {
			return name
		}
	}
	return yamlutil.ScalarString(node)
}

func parseVariables(node *yaml.Node) map[string]string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.MappingNode {
		return yamlutil.StringMap(node)
	}
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	values := map[string]string{}
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		name := yamlutil.ScalarString(yamlutil.MappingValue(item, "name"))
		if name == "" {
			continue
		}
		values[name] = yamlutil.ScalarString(yamlutil.MappingValue(item, "value"))
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func workflowUnsupported(root *yaml.Node) []model.FeatureSupport {
	features := []model.FeatureSupport{}
	if yamlutil.HasKey(root, "extends") {
		features = append(features, feature("Azure extends template", "extends", model.SupportUnsupported, "Azure extends templates are reported but not expanded locally."))
	}
	if yamlutil.HasKey(root, "resources") {
		features = append(features, feature("Azure resources", "resources", model.SupportUnsupported, "Repository, pipeline, package, and container resources are not fetched by the MVP."))
	}
	if yamlutil.HasKey(root, "parameters") {
		features = append(features, feature("Azure parameters", "parameters", model.SupportPartial, "Parameters are shown as YAML but are not evaluated with Azure template semantics."))
	}
	if yamlutil.HasKey(root, "schedules") {
		features = append(features, feature("Azure schedules", "schedules", model.SupportPartial, "Schedules are displayed but do not affect local runs."))
	}
	if yamlutil.HasKey(root, "stages") {
		features = append(features, feature("Azure stages", "stages", model.SupportPartial, "Stage ordering is approximated locally through dependency edges."))
	}
	return features
}

func jobUnsupported(id string, item *yaml.Node, isDeployment bool) []model.FeatureSupport {
	path := "jobs." + id
	features := []model.FeatureSupport{}
	if isDeployment {
		features = append(features, feature("Azure deployment job", path, model.SupportUnsupported, "Deployment jobs are parsed for visibility but not executed with Azure environment semantics."))
	}
	if yamlutil.HasKey(item, "container") {
		features = append(features, feature("Azure job container", path+".container", model.SupportUnsupported, "Azure job containers are not emulated by the MVP executor."))
	}
	if yamlutil.HasKey(item, "services") {
		features = append(features, feature("Azure services", path+".services", model.SupportUnsupported, "Service containers are not started by the MVP executor."))
	}
	if yamlutil.HasKey(item, "strategy") {
		features = append(features, feature("Azure strategy", path+".strategy", model.SupportUnsupported, "Matrix and parallel strategies are not expanded locally."))
	}
	if yamlutil.HasKey(item, "timeoutInMinutes") {
		features = append(features, feature("Azure timeout", path+".timeoutInMinutes", model.SupportPartial, "Azure job timeouts are displayed but not enforced locally."))
	}
	if yamlutil.HasKey(item, "continueOnError") {
		features = append(features, feature("Azure continueOnError", path+".continueOnError", model.SupportPartial, "continueOnError is displayed but does not change local run conclusions."))
	}
	return features
}

func feature(name, path string, support model.SupportLevel, message string) model.FeatureSupport {
	return model.FeatureSupport{
		Feature: name,
		Path:    path,
		Support: support,
		Message: message,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
