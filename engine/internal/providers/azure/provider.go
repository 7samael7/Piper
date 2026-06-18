package azure

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
	"github.com/7samael7/Piper/engine/internal/providers/yamlutil"
	"github.com/7samael7/Piper/engine/internal/support"
	"github.com/7samael7/Piper/engine/internal/workspace"
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
		RawYAML:  string(content),
		Jobs:     jobs,
		Features: azureWorkflowFeatures(root),
	}
	model.ApplySourceFile(workflow, path)
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
			ID:           fullID,
			Name:         firstNonEmpty(yamlutil.ScalarString(yamlutil.MappingValue(item, "displayName")), jobID),
			Runner:       pool,
			Stage:        stageID,
			Needs:        []string{},
			If:           yamlutil.ScalarString(yamlutil.MappingValue(item, "condition")),
			Env:          yamlutil.MergeStringMaps(inheritedVariables, parseVariables(yamlutil.MappingValue(item, "variables"))),
			Steps:        parseAzureJobSteps(item, fullID, isDeployment),
			Support:      model.SupportSupported,
			AllowFailure: yamlutil.ScalarBool(yamlutil.MappingValue(item, "continueOnError")),
			Environment:  yamlutil.ScalarString(yamlutil.MappingValue(item, "environment")),
			Origin:       &model.SourceOrigin{Line: item.Line, Column: item.Column},
			Features:     []model.FeatureRef{support.Ref("azure.metadata", "jobs."+fullID, azureOrigin(item))},
		}
		if yamlutil.MappingValue(item, "variables") != nil || len(inheritedVariables) > 0 {
			job.Features = append(job.Features, support.Ref("azure.variables", "jobs."+fullID+".variables", azureOrigin(yamlutil.MappingValue(item, "variables"))))
		}
		if yamlutil.MappingValue(item, "pool") != nil || inheritedPool != "" {
			job.Features = append(job.Features, support.Ref("azure.pool", "jobs."+fullID+".pool", azureOrigin(yamlutil.MappingValue(item, "pool"))))
		}
		if job.If != "" {
			job.Features = append(job.Features, support.Ref("azure.conditions", "jobs."+fullID+".condition", azureOrigin(yamlutil.MappingValue(item, "condition"))))
		}
		if job.If != "" {
			job.Condition = &model.ConditionSpec{Provider: model.ProviderAzure, Original: job.If, Kind: "condition"}
		}
		job.HasContainer = yamlutil.HasKey(item, "container")
		job.Services = parseServices(yamlutil.MappingValue(item, "services"))
		job.HasServices = len(job.Services) > 0
		job.Matrix, job.HasStrategy = parseStrategy(yamlutil.MappingValue(item, "strategy"))
		if isDeployment {
			job.HasStrategy = false
		}
		job.Features = append(job.Features, azureJobFeatures(fullID, item, isDeployment)...)
		if job.HasContainer || (job.HasStrategy && job.Matrix == nil) {
			job.Support = model.SupportUnsupported
		}
		if isDeployment {
			job.Support = model.CombineSupport(job.Support, model.SupportPartial)
		}
		if job.If != "" {
			job.Support = model.CombineSupport(job.Support, model.SupportPartial)
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
			If:               yamlutil.ScalarString(yamlutil.MappingValue(item, "condition")),
			ContinueOnError:  yamlutil.ScalarBool(yamlutil.MappingValue(item, "continueOnError")),
			Origin:           &model.SourceOrigin{Line: item.Line, Column: item.Column},
			Support:          model.SupportSupported,
		}
		if step.If != "" {
			step.Condition = &model.ConditionSpec{Provider: model.ProviderAzure, Original: step.If, Kind: "condition"}
			step.Features = append(step.Features, support.Ref("azure.conditions", stepPath+".condition", azureOrigin(yamlutil.MappingValue(item, "condition"))))
		}
		if yamlutil.MappingValue(item, "env") != nil {
			step.Features = append(step.Features, support.Ref("azure.variables", stepPath+".env", azureOrigin(yamlutil.MappingValue(item, "env"))))
		}
		if yamlutil.MappingValue(item, "workingDirectory") != nil {
			step.Features = append(step.Features, support.Ref("azure.working-directory", stepPath+".workingDirectory", azureOrigin(yamlutil.MappingValue(item, "workingDirectory"))))
		}
		if step.ContinueOnError {
			step.Features = append(step.Features, support.Ref("azure.continue-on-error", stepPath+".continueOnError", azureOrigin(yamlutil.MappingValue(item, "continueOnError"))))
		}
		switch {
		case yamlutil.HasKey(item, "bash"):
			step.Run = yamlutil.ScriptString(yamlutil.MappingValue(item, "bash"))
			step.Shell = "bash"
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = yamlutil.FirstLine(step.Run)
			}
			step.Features = append(step.Features, support.Ref("azure.scripts", stepPath+".bash", azureOrigin(yamlutil.MappingValue(item, "bash"))), support.Ref("common.shell", stepPath+".bash", azureOrigin(yamlutil.MappingValue(item, "bash"))))
		case yamlutil.HasKey(item, "script"):
			step.Run = yamlutil.ScriptString(yamlutil.MappingValue(item, "script"))
			step.Shell = "bash"
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = yamlutil.FirstLine(step.Run)
			}
			step.Features = append(step.Features, support.Ref("azure.scripts", stepPath+".script", azureOrigin(yamlutil.MappingValue(item, "script"))), support.Ref("common.shell", stepPath+".script", azureOrigin(yamlutil.MappingValue(item, "script"))))
		case yamlutil.HasKey(item, "checkout"):
			step.Uses = "checkout"
			step.Support = model.SupportPartial
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = "checkout " + yamlutil.ScalarString(yamlutil.MappingValue(item, "checkout"))
			}
			step.Features = append(step.Features, support.Ref("azure.checkout", stepPath+".checkout", azureOrigin(yamlutil.MappingValue(item, "checkout"))))
		case yamlutil.HasKey(item, "task"):
			step.Uses = yamlutil.ScalarString(yamlutil.MappingValue(item, "task"))
			step.With = yamlutil.StringMap(yamlutil.MappingValue(item, "inputs"))
			taskFeature := azureTaskFeature(step.Uses)
			step.Features = append(step.Features, support.Ref(taskFeature, stepPath+".task", azureOrigin(yamlutil.MappingValue(item, "task"))))
			normalizeAzureTask(&step)
			if step.Run == "" && step.Support == model.SupportUnsupported {
				step.Features = append(step.Features, support.Ref("azure.unknown-task", stepPath+".task", azureOrigin(yamlutil.MappingValue(item, "task"))))
			}
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = step.Uses
			}
		case yamlutil.HasKey(item, "template"):
			step.Uses = yamlutil.ScalarString(yamlutil.MappingValue(item, "template"))
			step.Support = model.SupportUnsupported
			step.Features = append(step.Features, support.Ref("azure.templates", stepPath+".template", azureOrigin(yamlutil.MappingValue(item, "template"))))
			if step.Name == fmt.Sprintf("Step %d", index+1) {
				step.Name = step.Uses
			}
		case yamlutil.HasKey(item, "pwsh"), yamlutil.HasKey(item, "powershell"):
			if yamlutil.HasKey(item, "pwsh") {
				step.Run = yamlutil.ScriptString(yamlutil.MappingValue(item, "pwsh"))
				step.Shell = "pwsh"
			} else {
				step.Run = yamlutil.ScriptString(yamlutil.MappingValue(item, "powershell"))
				step.Shell = "powershell"
			}
			step.Support = model.SupportPartial
			step.Features = append(step.Features, support.Ref("azure.scripts", stepPath, azureOrigin(item)), support.Ref("common.shell", stepPath, azureOrigin(item)))
		default:
			step.Support = model.SupportUnsupported
			step.Features = append(step.Features, support.Ref("common.empty-step", stepPath, azureOrigin(item)))
		}
		step.Features = append(step.Features, azureUnknownRefs(item, azureStepKeys, stepPath)...)
		steps = append(steps, step)
	}
	return steps
}

func normalizeAzureTask(step *model.Step) {
	task := strings.ToLower(step.Uses)
	inputs := step.With
	switch {
	case strings.HasPrefix(task, "bash@"):
		step.Run = firstNonEmpty(inputs["script"], inputs["inlineScript"], inputs["filePath"])
		step.Shell = "bash"
		step.Uses = ""
		step.Support = model.SupportSupported
	case strings.HasPrefix(task, "powershell@"):
		step.Run = firstNonEmpty(inputs["script"], inputs["inlineScript"], inputs["filePath"])
		step.Shell = "pwsh"
		step.Uses = ""
		step.Support = model.SupportPartial
	case strings.HasPrefix(task, "cmdline@"):
		step.Run = firstNonEmpty(inputs["script"], inputs["inlineScript"])
		step.Shell = "bash"
		step.Uses = ""
		step.Support = model.SupportPartial
	case strings.HasPrefix(task, "nodetool@"), strings.HasPrefix(task, "usenode@"):
		step.Uses = "actions/setup-node@local"
		step.With = map[string]string{"node-version": firstNonEmpty(inputs["versionSpec"], inputs["version"], "20")}
		step.Support = model.SupportPartial
	case strings.HasPrefix(task, "publishbuildartifacts@"),
		strings.HasPrefix(task, "downloadbuildartifacts@"),
		strings.HasPrefix(task, "cache@"):
		step.Support = model.SupportPartial
	default:
		step.Support = model.SupportUnsupported
	}
}

func parseStrategy(node *yaml.Node) (*model.MatrixSpec, bool) {
	if node == nil {
		return nil, false
	}
	matrix := yamlutil.MappingValue(node, "matrix")
	if matrix == nil || matrix.Kind != yaml.MappingNode {
		return nil, true
	}
	spec := &model.MatrixSpec{AzureLegs: map[string]map[string]string{}, FailFast: true}
	for i := 0; i+1 < len(matrix.Content); i += 2 {
		name := matrix.Content[i].Value
		spec.AzureLegs[name] = yamlutil.StringMap(matrix.Content[i+1])
	}
	if maxParallel := yamlutil.ScalarString(yamlutil.MappingValue(node, "maxParallel")); maxParallel != "" {
		fmt.Sscanf(maxParallel, "%d", &spec.MaxParallel)
	}
	return spec, true
}

func parseServices(node *yaml.Node) []model.ServiceSpec {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	services := []model.ServiceSpec{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		body := node.Content[i+1]
		service := model.ServiceSpec{Name: name, Image: yamlutil.ScalarString(body), Aliases: []string{name}, StartupTimeout: 60}
		if body.Kind == yaml.MappingNode {
			service.Image = yamlutil.ScalarString(yamlutil.MappingValue(body, "image"))
			service.Env = yamlutil.StringMap(yamlutil.MappingValue(body, "env"))
			service.Ports = yamlutil.StringSlice(yamlutil.MappingValue(body, "ports"))
		}
		if service.Image != "" {
			services = append(services, service)
		}
	}
	return services
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

func azureWorkflowFeatures(root *yaml.Node) []model.FeatureRef {
	features := []model.FeatureRef{support.Ref("azure.metadata", "workflow", azureOrigin(root))}
	if yamlutil.HasKey(root, "extends") {
		features = append(features, support.Ref("azure.templates", "extends", azureOrigin(yamlutil.MappingValue(root, "extends"))))
	}
	if yamlutil.HasKey(root, "resources") {
		features = append(features, support.Ref("azure.resources", "resources", azureOrigin(yamlutil.MappingValue(root, "resources"))))
	}
	if yamlutil.HasKey(root, "parameters") {
		features = append(features, support.Ref("azure.parameters", "parameters", azureOrigin(yamlutil.MappingValue(root, "parameters"))))
	}
	if yamlutil.HasKey(root, "schedules") || yamlutil.HasKey(root, "trigger") || yamlutil.HasKey(root, "pr") {
		features = append(features, support.Ref("azure.triggers", "triggers", azureOrigin(root)))
	}
	if yamlutil.HasKey(root, "variables") {
		features = append(features, support.Ref("azure.variables", "variables", azureOrigin(yamlutil.MappingValue(root, "variables"))))
	}
	if yamlutil.HasKey(root, "pool") {
		features = append(features, support.Ref("azure.pool", "pool", azureOrigin(yamlutil.MappingValue(root, "pool"))))
	}
	return append(features, azureUnknownRefs(root, azureWorkflowKeys, "")...)
}

func azureJobFeatures(id string, item *yaml.Node, isDeployment bool) []model.FeatureRef {
	path := "jobs." + id
	features := []model.FeatureRef{}
	if isDeployment {
		features = append(features, support.Ref("azure.deployment", path, azureOrigin(item)))
	}
	if yamlutil.HasKey(item, "container") {
		features = append(features, support.Ref("azure.job-container", path+".container", azureOrigin(yamlutil.MappingValue(item, "container"))))
	}
	if yamlutil.HasKey(item, "services") {
		features = append(features, support.Ref("azure.services", path+".services", azureOrigin(yamlutil.MappingValue(item, "services"))))
	}
	if yamlutil.HasKey(item, "strategy") {
		if matrix := yamlutil.MappingValue(yamlutil.MappingValue(item, "strategy"), "matrix"); matrix != nil {
			features = append(features, support.Ref("azure.matrix", path+".strategy", azureOrigin(yamlutil.MappingValue(item, "strategy"))))
		} else {
			features = append(features, support.Ref("azure.unknown", path+".strategy", azureOrigin(yamlutil.MappingValue(item, "strategy"))))
		}
	}
	if yamlutil.HasKey(item, "timeoutInMinutes") || yamlutil.HasKey(item, "cancelTimeoutInMinutes") {
		features = append(features, support.Ref("azure.timeout", path+".timeoutInMinutes", azureOrigin(item)))
	}
	if yamlutil.HasKey(item, "continueOnError") {
		features = append(features, support.Ref("azure.continue-on-error", path+".continueOnError", azureOrigin(yamlutil.MappingValue(item, "continueOnError"))))
	}
	return append(features, azureUnknownRefs(item, azureJobKeys, path)...)
}

func azureTaskFeature(task string) string {
	lower := strings.ToLower(task)
	switch {
	case strings.HasPrefix(lower, "publishbuildartifacts@"), strings.HasPrefix(lower, "downloadbuildartifacts@"), strings.HasPrefix(lower, "cache@"):
		return "azure.task-storage"
	case strings.HasPrefix(lower, "bash@"), strings.HasPrefix(lower, "powershell@"), strings.HasPrefix(lower, "cmdline@"),
		strings.HasPrefix(lower, "nodetool@"), strings.HasPrefix(lower, "usenode@"):
		return "azure.task-runtime"
	default:
		return "azure.unknown-task"
	}
}

var azureWorkflowKeys = map[string]bool{
	"name": true, "trigger": true, "pr": true, "schedules": true, "variables": true,
	"pool": true, "stages": true, "jobs": true, "steps": true, "parameters": true,
	"extends": true, "resources": true,
}

var azureJobKeys = map[string]bool{
	"job": true, "deployment": true, "displayName": true, "pool": true, "dependsOn": true,
	"condition": true, "variables": true, "steps": true, "continueOnError": true,
	"environment": true, "container": true, "services": true, "strategy": true,
	"timeoutInMinutes": true, "cancelTimeoutInMinutes": true,
}

var azureStepKeys = map[string]bool{
	"displayName": true, "env": true, "workingDirectory": true, "condition": true,
	"continueOnError": true, "bash": true, "script": true, "checkout": true,
	"task": true, "inputs": true, "template": true, "parameters": true,
	"pwsh": true, "powershell": true,
}

func azureUnknownRefs(node *yaml.Node, known map[string]bool, prefix string) []model.FeatureRef {
	result := []model.FeatureRef{}
	for index := 0; node != nil && index+1 < len(node.Content); index += 2 {
		key := node.Content[index]
		if known[key.Value] {
			continue
		}
		path := key.Value
		if prefix != "" {
			path = prefix + "." + key.Value
		}
		result = append(result, support.Ref("azure.unknown", path, azureOrigin(key)))
	}
	return result
}

func azureOrigin(node *yaml.Node) *model.SourceOrigin {
	if node == nil {
		return nil
	}
	return &model.SourceOrigin{Line: node.Line, Column: node.Column}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
