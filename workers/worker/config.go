package worker

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type WorkConfig struct {
	ID        string
	Name      WorkerName
	Timeout   time.Duration
	PerformAt *time.Time
	Retry     int
	Queue     WorkerQueue

	CreatedAt  time.Time
	RetryCount int
	Error      string
	WorkerRaw  []byte
}

func NewWorkConfig(name WorkerName, raw []byte) *WorkConfig {
	return &WorkConfig{
		ID:        uuid.NewString(),
		Name:      name,
		WorkerRaw: raw,
		Retry:     WorkerDefaultRetryCount,
		Timeout:   WorkerDefaultTimeout,
		Queue:     QueueLow,

		CreatedAt: time.Now(),
	}
}

func (c WorkConfig) Marshal() []byte {
	raw, err := json.Marshal(c)
	// impossible panic
	if err != nil {
		panic(err)
	}
	return raw
}

func UnMarshal(raw []byte) (*WorkConfig, error) {
	var c WorkConfig
	return &c, json.Unmarshal(raw, &c)
}
