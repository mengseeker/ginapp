package worker

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
)

type WorkerMarshaler func(Worker) ([]byte, error)
type WorkerUnMarshaler func([]byte) (Worker, error)

type Worker interface {
	Perform(ctx context.Context, logger *log.Logger)
}

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

var (
	WorkerDefaultRetryCount = 3
	WorkerDefaultTimeout    = 60 * time.Second
)

func NewJSONWorkerMarshaler() WorkerMarshaler {
	return func(w Worker) ([]byte, error) {
		return json.Marshal(w)
	}
}

func NewJSONWorkerUnMarshal(obj func() Worker) WorkerUnMarshaler {
	return func(b []byte) (Worker, error) {
		w := obj()
		return w, json.Unmarshal(b, w)
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

func new(name WorkerName, work Worker) (*WorkConfig, error) {
	wr, ok := RegistryWorkers[name]
	if !ok {
		return nil, ErrWorkerNotRegistry
	}
	raw, err := wr.WorkerMarshaler(work)
	if err != nil {
		return nil, err
	}
	return &WorkConfig{
		ID:        uuid.NewString(),
		Name:      name,
		WorkerRaw: raw,
		Retry:     WorkerDefaultRetryCount,
		Timeout:   WorkerDefaultTimeout,
		Queue:     QueueLow,

		CreatedAt: time.Now(),
	}, nil
}
