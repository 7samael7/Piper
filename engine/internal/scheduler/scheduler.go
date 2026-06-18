package scheduler

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

type Result struct {
	Status       model.RunStatus
	Outputs      map[string]string
	Error        error
	LogicalJobID string
}

type RunFunc func(context.Context, model.JobInstance, map[string]Result) Result
type StatusFunc func(model.JobInstance, model.RunStatus)

func Execute(ctx context.Context, instances []model.JobInstance, concurrency int, run RunFunc, status StatusFunc) (map[string]Result, error) {
	if concurrency <= 0 {
		concurrency = 1
	}
	jobs := make(map[string]model.JobInstance, len(instances))
	for _, instance := range instances {
		jobs[instance.ID] = instance
		if status != nil {
			status(instance, model.RunQueued)
		}
	}

	results := map[string]Result{}
	running := map[string]bool{}
	runningByLogical := map[string]int{}
	resultCh := make(chan struct {
		id     string
		result Result
	}, len(instances))
	var wg sync.WaitGroup

	for len(results) < len(instances) {
		if err := ctx.Err(); err != nil {
			wg.Wait()
			for id, instance := range jobs {
				if _, complete := results[id]; !complete {
					results[id] = Result{Status: model.RunCancelled, Error: err}
					if status != nil {
						status(instance, model.RunCancelled)
					}
				}
			}
			return results, err
		}

		ready := []model.JobInstance{}
		for id, instance := range jobs {
			if running[id] {
				continue
			}
			if _, complete := results[id]; complete {
				continue
			}
			allComplete := true
			for _, need := range instance.Needs {
				if _, complete := results[need]; !complete {
					allComplete = false
					break
				}
			}
			if allComplete {
				if instance.Job.Matrix != nil && instance.Job.Matrix.MaxParallel > 0 &&
					runningByLogical[instance.LogicalJobID] >= instance.Job.Matrix.MaxParallel {
					continue
				}
				ready = append(ready, instance)
			}
		}
		sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })

		for _, instance := range ready {
			if len(running) >= concurrency {
				break
			}
			running[instance.ID] = true
			runningByLogical[instance.LogicalJobID]++
			dependencies := map[string]Result{}
			for _, need := range instance.Needs {
				dependencies[need] = results[need]
			}
			if status != nil {
				status(instance, model.RunRunning)
			}
			wg.Add(1)
			go func(item model.JobInstance, needs map[string]Result) {
				defer wg.Done()
				resultCh <- struct {
					id     string
					result Result
				}{id: item.ID, result: run(ctx, item, needs)}
			}(instance, dependencies)
		}

		if len(running) == 0 {
			return results, fmt.Errorf("scheduler cannot make progress; dependency graph may contain a cycle")
		}

		select {
		case <-ctx.Done():
			continue
		case completed := <-resultCh:
			delete(running, completed.id)
			completedJob := jobs[completed.id]
			runningByLogical[completedJob.LogicalJobID]--
			if completed.result.Status == "" {
				if completed.result.Error != nil {
					completed.result.Status = model.RunFailed
				} else {
					completed.result.Status = model.RunSucceeded
				}
			}
			completed.result.LogicalJobID = jobs[completed.id].LogicalJobID
			results[completed.id] = completed.result
			if status != nil {
				status(jobs[completed.id], completed.result.Status)
			}
			if completed.result.Status == model.RunFailed && completedJob.Job.Matrix != nil && completedJob.Job.Matrix.FailFast {
				for id, sibling := range jobs {
					if sibling.LogicalJobID != completedJob.LogicalJobID || running[id] {
						continue
					}
					if _, complete := results[id]; complete {
						continue
					}
					results[id] = Result{Status: model.RunCancelled, Error: fmt.Errorf("matrix fail-fast cancelled this job after %s failed", completed.id)}
					if status != nil {
						status(sibling, model.RunCancelled)
					}
				}
			}
		}
	}
	wg.Wait()
	return results, nil
}
