package workers

import (
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

func (w *ExampleWorker) WorkerName() string {
	return "ExampleWorker"
}

func (w *ExampleWorker) Perform(ctx worker.Context, l *log.Logger) error {
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
