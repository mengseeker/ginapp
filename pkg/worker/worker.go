package worker

import (
	"context"
	"encoding/json"
	"ginapp/pkg/log"
	"time"
)

type Worker interface {
	Perform(ctx context.Context, logger *log.Logger) error
}

type WorkerName string

type WorkerQueue string

const (
	QueueHigh WorkerQueue = "workerQueueHigh"
	QueueLow  WorkerQueue = "workerQueueLow"
)

var (
	WorkerDefaultRetryCount = 13
	WorkerDefaultTimeout    = 60 * time.Second
)

type WorkerMarshaler func(Worker) ([]byte, error)
type WorkerUnMarshaler func([]byte) (Worker, error)

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
