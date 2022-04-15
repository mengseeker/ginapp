package worker

import (
	"errors"
	"fmt"
)

type workerRegistry struct {
	WorkerMarshaler   WorkerMarshaler
	WorkerUnMarshaler WorkerUnMarshaler
}

type WorkerName string

type WorkerQueue string

const (
	QueueHigh WorkerQueue = "workerQueueHigh"
	QueueLow  WorkerQueue = "workerQueueLow"
)

var (
	RegistryWorkers = map[WorkerName]workerRegistry{}

	ErrWorkerNotRegistry = errors.New("unregistry worker")
)

// worker should registry before worker loop lanch
func RegistryWorker(name WorkerName, m WorkerMarshaler, um WorkerUnMarshaler) {
	if _, exist := RegistryWorkers[name]; exist {
		panic(fmt.Errorf("worker %s has already registry", name))
	}
	RegistryWorkers[name] = workerRegistry{
		WorkerMarshaler:   m,
		WorkerUnMarshaler: um,
	}
}
