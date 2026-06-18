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
	fullPath, err := workspace.Resolve(repoPath, cleanPath)
	if err != nil {
		return nil, nil, err
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, nil, err
	}
	resolvedContent, err := resolveConfiguration(repoPath, filepath.ToSlash(cleanPath), content)
	if err != nil {
		return nil, content, err
	}
	workflow, err := parseWorkflow(filepath.ToSlash(cleanPath), resolvedContent)
	if err != nil {
		return nil, content, err
	}
	workflow.RawYAML = string(content)
	workflow.ResolvedYAML = string(resolvedContent)
	if originalRoot, rootErr := yamlutil.RootMapping(content); rootErr == nil {
		workflow.Features = append(workflow.Features, gitlabCompositionFeatures(originalRoot)...)
	}
	model.ApplySourceFile(workflow, filepath.ToSlash(cleanPath))
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
		RawYAML:  string(content),
		Jobs:     jobs,
		Features: gitlabWorkflowFeatures(root),
	}
	model.ApplySourceFile(workflow, path)
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
			ID:           id,
			Name:         id,
			Runner:       runner,
			Stage:        stage,
			Image:        image,
			Needs:        needs,
			If:           gitlabCondition(body),
			Env:          yamlutil.MergeStringMaps(globalVariables, yamlutil.StringMap(yamlutil.MappingValue(body, "variables"))),
			Steps:        gitlabSteps(globalBefore, yamlutil.ScriptString(yamlutil.MappingValue(body, "before_script")), yamlutil.ScriptString(yamlutil.MappingValue(body, "script")), yamlutil.ScriptString(yamlutil.MappingValue(body, "after_script")), globalAfter),
			Support:      model.SupportSupported,
			AllowFailure: yamlutil.ScalarBool(yamlutil.MappingValue(body, "allow_failure")),
			When:         yamlutil.ScalarString(yamlutil.MappingValue(body, "when")),
			Environment:  gitlabEnvironment(yamlutil.MappingValue(body, "environment")),
			Origin:       &model.SourceOrigin{Line: root.Content[i].Line, Column: root.Content[i].Column},
			Features:     []model.FeatureRef{support.Ref("gitlab.metadata", "jobs."+id, gitlabOrigin(root.Content[i]))},
		}
		if yamlutil.MappingValue(body, "variables") != nil || len(globalVariables) > 0 {
			job.Features = append(job.Features, support.Ref("gitlab.variables", "jobs."+id+".variables", gitlabOrigin(yamlutil.MappingValue(body, "variables"))))
		}
		if yamlutil.MappingValue(body, "image") != nil || image != "" || yamlutil.MappingValue(body, "tags") != nil {
			job.Features = append(job.Features, support.Ref("gitlab.image-tags", "jobs."+id, gitlabOrigin(body)))
		}
		if job.If != "" {
			job.Features = append(job.Features, support.Ref("gitlab.rules", "jobs."+id, gitlabOrigin(body)))
		}
		if job.If != "" {
			job.Condition = &model.ConditionSpec{Provider: model.ProviderGitLab, Original: job.If, Kind: "rules"}
		}
		job.Services = parseServices(yamlutil.MappingValue(body, "services"))
		job.HasServices = len(job.Services) > 0
		job.Artifacts = parseArtifacts(yamlutil.MappingValue(body, "artifacts"), id)
		job.Caches = parseCaches(yamlutil.MappingValue(body, "cache"))
		job.HasStrategy = yamlutil.HasKey(body, "parallel")
		if trigger := yamlutil.ScalarString(yamlutil.MappingValue(body, "trigger")); trigger != "" {
			job.ReusableWorkflow = trigger
		} else if triggerNode := yamlutil.MappingValue(body, "trigger"); triggerNode != nil {
			job.ReusableWorkflow = yamlutil.YAMLToString(triggerNode)
		}
		job.Features = append(job.Features, gitlabJobFeatures(id, body)...)
		if job.HasStrategy || job.ReusableWorkflow != "" {
			job.Support = model.SupportUnsupported
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
			Features: []model.FeatureRef{
				supportpkgRef("gitlab.scripts", "script"),
				supportpkgRef("common.shell", "script"),
			},
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
	if rules := yamlutil.MappingValue(body, "rules"); rules != nil && rules.Kind == yaml.SequenceNode {
		for _, rule := range rules.Content {
			if rule.Kind != yaml.MappingNode {
				continue
			}
			if expression := yamlutil.ScalarString(yamlutil.MappingValue(rule, "if")); expression != "" {
				return expression
			}
			if changes := yamlutil.MappingValue(rule, "changes"); changes != nil {
				patterns := yamlutil.StringSlice(changes)
				parts := make([]string, 0, len(patterns))
				for _, pattern := range patterns {
					parts = append(parts, fmt.Sprintf("changed(%q)", pattern))
				}
				if len(parts) > 0 {
					return strings.Join(parts, " || ")
				}
			}
			when := yamlutil.ScalarString(yamlutil.MappingValue(rule, "when"))
			if when == "never" {
				return "false"
			}
			return "true"
		}
	}
	only := gitlabRefExpression(yamlutil.MappingValue(body, "only"))
	except := gitlabRefExpression(yamlutil.MappingValue(body, "except"))
	switch {
	case only != "" && except != "":
		return "(" + only + ") && !(" + except + ")"
	case only != "":
		return only
	case except != "":
		return "!(" + except + ")"
	default:
		return ""
	}
}

func gitlabRefExpression(node *yaml.Node) string {
	values := yamlutil.StringSlice(node)
	parts := []string{}
	for _, value := range values {
		switch value {
		case "branches":
			parts = append(parts, "env.CI_COMMIT_BRANCH != ''")
		case "tags":
			parts = append(parts, "env.CI_COMMIT_TAG != ''")
		default:
			parts = append(parts, fmt.Sprintf("matches(env.CI_COMMIT_REF_NAME, %q)", value))
		}
	}
	return strings.Join(parts, " || ")
}

func parseServices(node *yaml.Node) []model.ServiceSpec {
	if node == nil {
		return nil
	}
	items := node.Content
	if node.Kind != yaml.SequenceNode {
		items = []*yaml.Node{node}
	}
	services := []model.ServiceSpec{}
	for index, item := range items {
		service := model.ServiceSpec{Image: yamlutil.ScalarString(item), StartupTimeout: 60}
		if item.Kind == yaml.MappingNode {
			service.Image = yamlutil.ScalarString(yamlutil.MappingValue(item, "name"))
			service.Name = yamlutil.ScalarString(yamlutil.MappingValue(item, "alias"))
			service.Aliases = yamlutil.StringSlice(yamlutil.MappingValue(item, "aliases"))
			service.Env = yamlutil.StringMap(yamlutil.MappingValue(item, "variables"))
		}
		if service.Name == "" {
			service.Name = fmt.Sprintf("service-%d", index+1)
		}
		if len(service.Aliases) == 0 {
			service.Aliases = []string{service.Name}
		}
		if service.Image != "" {
			services = append(services, service)
		}
	}
	return services
}

func parseArtifacts(node *yaml.Node, jobID string) []model.ArtifactSpec {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	paths := yamlutil.StringSlice(yamlutil.MappingValue(node, "paths"))
	if len(paths) == 0 {
		return nil
	}
	name := yamlutil.ScalarString(yamlutil.MappingValue(node, "name"))
	if name == "" {
		name = jobID
	}
	return []model.ArtifactSpec{{
		Name: name, Paths: paths,
		When:     yamlutil.ScalarString(yamlutil.MappingValue(node, "when")),
		ExpireIn: yamlutil.ScalarString(yamlutil.MappingValue(node, "expire_in")),
	}}
}

func parseCaches(node *yaml.Node) []model.CacheSpec {
	if node == nil {
		return nil
	}
	items := node.Content
	if node.Kind != yaml.SequenceNode {
		items = []*yaml.Node{node}
	}
	result := []model.CacheSpec{}
	for _, item := range items {
		if item.Kind != yaml.MappingNode {
			continue
		}
		result = append(result, model.CacheSpec{
			Key:    yamlutil.ScalarString(yamlutil.MappingValue(item, "key")),
			Paths:  yamlutil.StringSlice(yamlutil.MappingValue(item, "paths")),
			Policy: yamlutil.ScalarString(yamlutil.MappingValue(item, "policy")),
		})
	}
	return result
}

func gitlabEnvironment(node *yaml.Node) string {
	if node != nil && node.Kind == yaml.MappingNode {
		return yamlutil.ScalarString(yamlutil.MappingValue(node, "name"))
	}
	return yamlutil.ScalarString(node)
}

func gitlabWorkflowFeatures(root *yaml.Node) []model.FeatureRef {
	features := []model.FeatureRef{support.Ref("gitlab.metadata", "workflow", gitlabOrigin(root))}
	if yamlutil.HasKey(root, "workflow") {
		features = append(features, support.Ref("gitlab.workflow-rules", "workflow", gitlabOrigin(yamlutil.MappingValue(root, "workflow"))))
	}
	if yamlutil.HasKey(root, "default") {
		features = append(features, support.Ref("gitlab.default", "default", gitlabOrigin(yamlutil.MappingValue(root, "default"))))
	}
	if yamlutil.HasKey(root, "variables") {
		features = append(features, support.Ref("gitlab.variables", "variables", gitlabOrigin(yamlutil.MappingValue(root, "variables"))))
	}
	if yamlutil.HasKey(root, "image") {
		features = append(features, support.Ref("gitlab.image-tags", "image", gitlabOrigin(yamlutil.MappingValue(root, "image"))))
	}
	return append(features, gitlabCompositionFeatures(root)...)
}

func gitlabJobFeatures(id string, body *yaml.Node) []model.FeatureRef {
	path := "jobs." + id
	features := []model.FeatureRef{}
	if yamlutil.HasKey(body, "extends") {
		features = append(features, support.Ref("gitlab.extends", path+".extends", gitlabOrigin(yamlutil.MappingValue(body, "extends"))))
	}
	if yamlutil.HasKey(body, "services") {
		features = append(features, support.Ref("gitlab.services", path+".services", gitlabOrigin(yamlutil.MappingValue(body, "services"))))
	}
	if yamlutil.HasKey(body, "parallel") {
		features = append(features, support.Ref("gitlab.parallel", path+".parallel", gitlabOrigin(yamlutil.MappingValue(body, "parallel"))))
	}
	if yamlutil.HasKey(body, "trigger") {
		features = append(features, support.Ref("gitlab.child-pipeline", path+".trigger", gitlabOrigin(yamlutil.MappingValue(body, "trigger"))))
	}
	for _, key := range []string{"dependencies", "resource_group", "coverage", "retry", "timeout"} {
		if yamlutil.HasKey(body, key) {
			features = append(features, support.Ref("gitlab.unsupported-job-policy", path+"."+key, gitlabOrigin(yamlutil.MappingValue(body, key))))
		}
	}
	for _, key := range []string{"artifacts", "cache"} {
		if yamlutil.HasKey(body, key) {
			features = append(features, support.Ref("gitlab.artifacts-cache", path+"."+key, gitlabOrigin(yamlutil.MappingValue(body, key))))
		}
	}
	if yamlutil.HasKey(body, "environment") || yamlutil.ScalarString(yamlutil.MappingValue(body, "when")) == "manual" {
		features = append(features, support.Ref("gitlab.environment-manual", path, gitlabOrigin(body)))
	}
	if yamlutil.HasKey(body, "allow_failure") {
		features = append(features, support.Ref("gitlab.allow-failure", path+".allow_failure", gitlabOrigin(yamlutil.MappingValue(body, "allow_failure"))))
	}
	features = append(features, gitlabUnknownRefs(body, path)...)
	return features
}

func gitlabCompositionFeatures(root *yaml.Node) []model.FeatureRef {
	result := []model.FeatureRef{}
	include := yamlutil.MappingValue(root, "include")
	if include != nil {
		local, remote := classifyIncludes(include)
		if local {
			result = append(result, support.Ref("gitlab.include-local", "include", gitlabOrigin(include)))
		}
		if remote {
			result = append(result, support.Ref("gitlab.include-remote", "include", gitlabOrigin(include)))
		}
	}
	for index := 0; root != nil && index+1 < len(root.Content); index += 2 {
		id := root.Content[index].Value
		body := root.Content[index+1]
		if gitlabReservedKeys[id] || body.Kind != yaml.MappingNode || !yamlutil.HasKey(body, "extends") {
			continue
		}
		result = append(result, support.Ref("gitlab.extends", "jobs."+id+".extends", gitlabOrigin(yamlutil.MappingValue(body, "extends"))))
	}
	return result
}

func classifyIncludes(node *yaml.Node) (local, remote bool) {
	if node == nil {
		return false, false
	}
	items := node.Content
	if node.Kind != yaml.SequenceNode {
		items = []*yaml.Node{node}
	}
	for _, item := range items {
		switch item.Kind {
		case yaml.ScalarNode:
			local = true
		case yaml.MappingNode:
			if yamlutil.HasKey(item, "local") {
				local = true
			} else {
				remote = true
			}
		}
	}
	return
}

var gitlabKnownJobKeys = map[string]bool{
	"stage": true, "needs": true, "image": true, "tags": true, "variables": true,
	"before_script": true, "script": true, "after_script": true, "rules": true,
	"only": true, "except": true, "services": true, "artifacts": true, "cache": true,
	"parallel": true, "trigger": true, "dependencies": true, "resource_group": true,
	"coverage": true, "retry": true, "timeout": true, "environment": true,
	"allow_failure": true, "when": true, "extends": true,
}

func gitlabUnknownRefs(node *yaml.Node, prefix string) []model.FeatureRef {
	result := []model.FeatureRef{}
	for index := 0; node != nil && index+1 < len(node.Content); index += 2 {
		key := node.Content[index]
		if !gitlabKnownJobKeys[key.Value] {
			result = append(result, support.Ref("gitlab.unknown", prefix+"."+key.Value, gitlabOrigin(key)))
		}
	}
	return result
}

func gitlabOrigin(node *yaml.Node) *model.SourceOrigin {
	if node == nil {
		return nil
	}
	return &model.SourceOrigin{Line: node.Line, Column: node.Column}
}

func supportpkgRef(id, path string) model.FeatureRef {
	return support.Ref(id, path, nil)
}
