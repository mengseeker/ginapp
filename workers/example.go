package workers

import (
	"context"
	"errors"
	"fmt"
	"ginapp/pkg/log"
	"ginapp/workers/worker"
	"time"
)

type ExampleWorker struct{}

func (b *ExampleWorker) Perform(ctx context.Context, l *log.Logger) error {
	l.Info("BaiduWorker running")
	return errors.New("retry")
}

func TestExample() {
	w, err := Runner.Declare(
		"ExampleWorker",
		&ExampleWorker{},
		worker.WithRetry(3),
		worker.WithPerformAt(time.Now().Add(time.Second*3)),
		worker.WithQueue(worker.QueueHigh),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(w.ID)
}
