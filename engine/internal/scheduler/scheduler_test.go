package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestExecuteRunsIndependentJobsConcurrently(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	jobs := []model.JobInstance{
		{ID: "a"}, {ID: "b"}, {ID: "c", Needs: []string{"a", "b"}},
	}
	results, err := Execute(context.Background(), jobs, 2, func(_ context.Context, job model.JobInstance, _ map[string]Result) Result {
		now := active.Add(1)
		for {
			old := maximum.Load()
			if now <= old || maximum.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		return Result{Status: model.RunSucceeded}
	}, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(results) != 3 || maximum.Load() != 2 {
		t.Fatalf("results=%d max=%d", len(results), maximum.Load())
	}
}
