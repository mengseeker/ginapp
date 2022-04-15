package worker

import (
	"context"
	"errors"
	"fmt"
	"ginapp/pkg/log"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
)

type Runner interface {
	Declare(name WorkerName, work Worker, opts ...WorkerOption) (*WorkConfig, error)
	RegistryWorker(name WorkerName, m WorkerMarshaler, um WorkerUnMarshaler)
	RunLoop(logger *log.Logger) error
}

var (
	_ Runner = &RedisRunner{}

	ErrWorkerNotRegistry = errors.New("unregistry worker")

	RedisPrefix = "worker_"

	RedisKeyWokers           = RedisPrefix + "workers" // 存储work数据
	RedisKeyWaitQueue        = RedisPrefix + "wait"    // 待调度
	RedisKeyReadyQueuePrefix = RedisPrefix + "ready_"  // 就绪队列
)

type RedisRunner struct {
	redisCli        redis.Conn
	redisLocker     sync.Mutex
	RegistryWorkers map[WorkerName]workerRegistry
}

type workerRegistry struct {
	WorkerMarshaler   WorkerMarshaler
	WorkerUnMarshaler WorkerUnMarshaler
}

func NewRedisRunner(redisUrl string, opts ...redis.DialOption) (*RedisRunner, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	redisCli, err := redis.DialURLContext(ctx, redisUrl, opts...)
	if err != nil {
		return nil, err
	}
	return &RedisRunner{
		redisCli:        redisCli,
		RegistryWorkers: map[WorkerName]workerRegistry{},
	}, nil
}

// Declare should used before worker Registry
func (r *RedisRunner) Declare(name WorkerName, work Worker, opts ...WorkerOption) (*WorkConfig, error) {
	c, err := r.newConfig(name, work)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, r.postToRedis(c)
}

func (r *RedisRunner) postToRedis(c *WorkConfig) error {
	r.redisLocker.Lock()
	defer r.redisLocker.Unlock()
	raw := c.Marshal()
	r.redisCli.Send("MULTI")
	r.redisCli.Send("HSET", RedisKeyWokers, c.ID, raw)
	if c.PerformAt != nil && c.PerformAt.After(time.Now()) {
		r.redisCli.Send("ZADD", RedisKeyWaitQueue, c.PerformAt.Unix(), c.ID)
	} else {
		r.redisCli.Send("LPUSH", RedisKeyReadyQueuePrefix+string(c.Queue), c.ID)
	}
	_, err := r.redisCli.Do("EXEC")
	// reply, err := r.redisCli.Do("EXEC")
	// fmt.Println("============", reply)
	if err != nil {
		return err
	}
	return nil
}

// worker should registry before worker loop lanch
func (r *RedisRunner) RegistryWorker(name WorkerName, m WorkerMarshaler, um WorkerUnMarshaler) {
	if _, exist := r.RegistryWorkers[name]; exist {
		panic(fmt.Errorf("worker %s has already registry", name))
	}
	r.RegistryWorkers[name] = workerRegistry{
		WorkerMarshaler:   m,
		WorkerUnMarshaler: um,
	}
}

// TODO
func (r *RedisRunner) RunLoop(logger *log.Logger) error {
	<-time.After(time.Second * 30)
	return errors.New("test")
}

func (r *RedisRunner) newConfig(name WorkerName, work Worker) (*WorkConfig, error) {
	wr, ok := r.RegistryWorkers[name]
	if !ok {
		return nil, ErrWorkerNotRegistry
	}
	raw, err := wr.WorkerMarshaler(work)
	if err != nil {
		return nil, err
	}
	return NewWorkConfig(name, raw), nil
}
