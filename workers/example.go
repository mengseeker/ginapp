package workers

import (
	"context"
	"errors"
	"ginapp/pkg/log"
	"ginapp/pkg/worker"
	"time"
)

type ExampleWorker struct {
	Timeout *time.Duration
	Panic   bool
	Error   string
}

func (w *ExampleWorker) Perform(ctx context.Context, l *log.Logger) error {
	l.Infof("ExampleWorker running %#v", *w)
	if w.Timeout != nil {
		time.Sleep(*w.Timeout)
	}
	if w.Panic {
		panic("ExampleWorker")
	}
	if w.Error != "" {
		return errors.New(w.Error)
	}
	return nil
}

func (w *ExampleWorker) Declare(opts ...worker.WorkerOption) (*worker.WorkConfig, error) {
	return Runner.Declare(
		"ExampleWorker",
		w,
		opts...,
	)
}
