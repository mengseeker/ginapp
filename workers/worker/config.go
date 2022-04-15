package worker

import (
	"encoding/json"
	"time"
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
