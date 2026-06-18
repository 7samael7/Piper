package graph

import (
	"fmt"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func Build(workflow *model.Workflow) model.Graph {
	nodes := make([]model.GraphNode, 0, len(workflow.Jobs))
	edges := make([]model.GraphEdge, 0)
	for _, job := range workflow.Jobs {
		nodes = append(nodes, model.GraphNode{
			ID:      job.ID,
			Label:   job.Name,
			Support: job.Support,
		})
		for _, need := range job.Needs {
			edges = append(edges, model.GraphEdge{
				ID:     fmt.Sprintf("%s-%s", need, job.ID),
				Source: need,
				Target: job.ID,
			})
		}
	}
	return model.Graph{Nodes: nodes, Edges: edges}
}

func BuildExecutionPlan(plan *model.ExecutionPlan) model.Graph {
	if plan == nil {
		return model.Graph{}
	}
	nodes := make([]model.GraphNode, 0, len(plan.Jobs))
	edges := []model.GraphEdge{}
	for _, instance := range plan.Jobs {
		nodes = append(nodes, model.GraphNode{
			ID: instance.ID, Label: instance.Name, Support: instance.Job.Support,
			LogicalJobID: instance.LogicalJobID, Matrix: instance.Matrix,
		})
		for _, need := range instance.Needs {
			edges = append(edges, model.GraphEdge{
				ID: need + "-" + instance.ID, Source: need, Target: instance.ID,
			})
		}
	}
	return model.Graph{Nodes: nodes, Edges: edges}
}

func TopologicalSort(workflow *model.Workflow) ([]model.Job, error) {
	jobsByID := make(map[string]model.Job, len(workflow.Jobs))
	inDegree := make(map[string]int, len(workflow.Jobs))
	dependents := make(map[string][]string, len(workflow.Jobs))

	for _, job := range workflow.Jobs {
		jobsByID[job.ID] = job
		inDegree[job.ID] = 0
	}

	for _, job := range workflow.Jobs {
		for _, need := range job.Needs {
			if _, ok := jobsByID[need]; !ok {
				return nil, fmt.Errorf("job %q needs missing job %q", job.ID, need)
			}
			inDegree[job.ID]++
			dependents[need] = append(dependents[need], job.ID)
		}
	}

	queue := make([]string, 0)
	for _, job := range workflow.Jobs {
		if inDegree[job.ID] == 0 {
			queue = append(queue, job.ID)
		}
	}

	ordered := make([]model.Job, 0, len(workflow.Jobs))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		ordered = append(ordered, jobsByID[id])
		for _, dependent := range dependents[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(ordered) != len(workflow.Jobs) {
		return nil, fmt.Errorf("workflow contains a dependency cycle")
	}

	return ordered, nil
}
