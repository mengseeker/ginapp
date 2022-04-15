package workers

import (
	"ginapp/pkg/log"
	"ginapp/workers/worker"

	"github.com/gomodule/redigo/redis"
)

var (
	Runner worker.Runner

	logger = log.NewLogger()
)

// 初始化worker
func Initialize(redisUrl string, opts ...redis.DialOption) error {
	var err error
	Runner, err = worker.NewRedisRunner(redisUrl, opts...)
	if err != nil {
		return err
	}
	RegistryWorkers()
	return nil
}

func RegistryWorkers() {
	Runner.RegistryWorker("ExampleWorker", worker.NewJSONWorkerMarshaler(),
		worker.NewJSONWorkerUnMarshal(func() worker.Worker { return &ExampleWorker{} }),
	)
}

// 启动worker循环
func RunLoop() error {
	return Runner.RunLoop(logger)
}
