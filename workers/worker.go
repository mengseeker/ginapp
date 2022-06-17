package workers

import (
	"context"
	"fmt"
	"ginapp/pkg/worker"
	"time"

	"github.com/go-redis/redis/v8"
)

var (
	runner *worker.RedisRunner
)

// 初始化worker
func Initialize(redisUrl string) error {
	opt, err := redis.ParseURL(redisUrl)
	if err != nil {
		return err
	}
	cli := redis.NewClient(opt)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	err = cli.Ping(ctx).Err()
	if err != nil {
		return err
	}
	runner, err = worker.NewRunner(cli, 3)
	if err != nil {
		return err
	}
	RegistryWorkers()
	return nil
}

func RegistryWorkers() {
	var err error
	workers := []worker.Worker{
		&ExampleWorker{},
	}
	for _, w := range workers {
		err = runner.RegistryWorker(w)
		if err != nil {
			panic(fmt.Errorf("register worker %s error: %s", w.WorkerName(), err))
		}
	}
}

// 启动worker循环
func Run() error {
	return runner.Run(context.Background())
}

func DeclareWorker(w worker.Worker, opts ...worker.Option) (string, error) {
	m, err := runner.Declare(w, opts...)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}
