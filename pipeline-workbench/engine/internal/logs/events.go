package logs

import (
	"time"

	"github.com/pipeline-workbench/engine/internal/pipeline/model"
)

type Event struct {
	RunID   string                 `json:"runId"`
	Time    time.Time              `json:"time"`
	Type    string                 `json:"type"`
	JobID   string                 `json:"jobId,omitempty"`
	StepID  string                 `json:"stepId,omitempty"`
	Stream  string                 `json:"stream,omitempty"`
	Status  model.RunStatus        `json:"status,omitempty"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

type Emitter func(Event)

func New(runID, eventType, message string) Event {
	return Event{
		RunID:   runID,
		Time:    time.Now().UTC(),
		Type:    eventType,
		Stream:  "system",
		Message: message,
	}
}
