package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

const (
	DefaultMaxExpandedJobs = 128
	DefaultConcurrency     = 4
)

func Compile(workflow *model.Workflow, maxExpandedJobs, concurrency int) (*model.ExecutionPlan, error) {
	if maxExpandedJobs <= 0 {
		maxExpandedJobs = DefaultMaxExpandedJobs
	}
	if maxExpandedJobs > 1024 {
		maxExpandedJobs = 1024
	}
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	if concurrency > 64 {
		concurrency = 64
	}

	instancesByLogical := map[string][]model.JobInstance{}
	orderedLogical := make([]string, 0, len(workflow.Jobs))
	for _, job := range workflow.Jobs {
		instances, err := expandJob(job)
		if err != nil {
			return nil, err
		}
		instancesByLogical[job.ID] = instances
		orderedLogical = append(orderedLogical, job.ID)
	}

	total := 0
	for _, instances := range instancesByLogical {
		total += len(instances)
	}
	if total > maxExpandedJobs {
		return nil, fmt.Errorf("matrix expansion produced %d jobs, exceeding the configured limit of %d", total, maxExpandedJobs)
	}

	result := make([]model.JobInstance, 0, total)
	for _, logicalID := range orderedLogical {
		for _, instance := range instancesByLogical[logicalID] {
			needs := []string{}
			for _, need := range instance.Job.Needs {
				dependencies, ok := instancesByLogical[need]
				if !ok {
					return nil, fmt.Errorf("job %q needs missing job %q", logicalID, need)
				}
				for _, dependency := range dependencies {
					needs = append(needs, dependency.ID)
				}
			}
			instance.Needs = needs
			instance.Job.Needs = append([]string(nil), needs...)
			result = append(result, instance)
		}
	}

	return &model.ExecutionPlan{
		Jobs:            result,
		MaxConcurrency:  concurrency,
		MaxExpandedJobs: maxExpandedJobs,
	}, nil
}

func expandJob(job model.Job) ([]model.JobInstance, error) {
	if job.Matrix == nil {
		return []model.JobInstance{{
			ID:           job.ID,
			LogicalJobID: job.ID,
			Name:         job.Name,
			Job:          job,
		}}, nil
	}
	if len(job.Matrix.AzureLegs) > 0 {
		names := make([]string, 0, len(job.Matrix.AzureLegs))
		for name := range job.Matrix.AzureLegs {
			names = append(names, name)
		}
		sort.Strings(names)
		instances := make([]model.JobInstance, 0, len(names))
		for _, name := range names {
			values := map[string]interface{}{}
			for key, value := range job.Matrix.AzureLegs[name] {
				values[key] = value
			}
			instances = append(instances, instance(job, values, name))
		}
		return instances, nil
	}

	keys := make([]string, 0, len(job.Matrix.Dimensions))
	for key := range job.Matrix.Dimensions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	combinations := []map[string]interface{}{{}}
	for _, key := range keys {
		values := job.Matrix.Dimensions[key]
		if len(values) == 0 {
			return nil, fmt.Errorf("job %s matrix dimension %s has no values", job.ID, key)
		}
		next := make([]map[string]interface{}, 0, len(combinations)*len(values))
		for _, combination := range combinations {
			for _, value := range values {
				item := cloneMap(combination)
				item[key] = value
				next = append(next, item)
			}
		}
		combinations = next
	}

	filtered := combinations[:0]
	for _, combination := range combinations {
		excluded := false
		for _, exclude := range job.Matrix.Exclude {
			if matches(combination, exclude) {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, combination)
		}
	}
	combinations = filtered

	for _, include := range job.Matrix.Include {
		merged := false
		for index := range combinations {
			if compatible(combinations[index], include, keys) {
				combinations[index] = merge(combinations[index], include)
				merged = true
			}
		}
		if !merged {
			combinations = append(combinations, cloneMap(include))
		}
	}

	instances := make([]model.JobInstance, 0, len(combinations))
	for _, values := range combinations {
		instances = append(instances, instance(job, values, ""))
	}
	sort.SliceStable(instances, func(i, j int) bool { return instances[i].ID < instances[j].ID })
	return instances, nil
}

func instance(job model.Job, values map[string]interface{}, legName string) model.JobInstance {
	labelParts := []string{}
	if legName != "" {
		labelParts = append(labelParts, legName)
	} else {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			labelParts = append(labelParts, fmt.Sprintf("%s=%v", key, values[key]))
		}
	}
	name := job.Name
	if len(labelParts) > 0 {
		name += " (" + strings.Join(labelParts, ", ") + ")"
	}
	encoded, _ := json.Marshal(values)
	sum := sha256.Sum256(encoded)
	id := job.ID + "-" + hex.EncodeToString(sum[:])[:10]
	instanceJob := job
	instanceJob.ID = id
	instanceJob.Name = name
	return model.JobInstance{
		ID:           id,
		LogicalJobID: job.ID,
		Name:         name,
		Matrix:       cloneMap(values),
		Job:          instanceJob,
	}
}

func matches(values, pattern map[string]interface{}) bool {
	for key, expected := range pattern {
		actual, ok := values[key]
		if !ok || fmt.Sprint(actual) != fmt.Sprint(expected) {
			return false
		}
	}
	return true
}

func compatible(values, include map[string]interface{}, dimensionKeys []string) bool {
	dimensions := map[string]bool{}
	for _, key := range dimensionKeys {
		dimensions[key] = true
	}
	for key, expected := range include {
		if dimensions[key] {
			if actual, ok := values[key]; ok && fmt.Sprint(actual) != fmt.Sprint(expected) {
				return false
			}
		}
	}
	return true
}

func merge(left, right map[string]interface{}) map[string]interface{} {
	result := cloneMap(left)
	for key, value := range right {
		result[key] = value
	}
	return result
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	result := make(map[string]interface{}, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
