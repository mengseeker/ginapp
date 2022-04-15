package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/google/uuid"
)

type Runner interface {
	Declare(name WorkerName, work Worker, opts ...WorkerOption) (*WorkConfig, error)
	RegistryWorker(name WorkerName, m WorkerMarshaler, um WorkerUnMarshaler)
}

var (
	_ Runner = &RedisRunner{}

	ErrWorkerNotRegistry = errors.New("unregistry worker")
)

type RedisRunner struct {
	redisCli        redis.Conn
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
		redisCli: redisCli,
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
	// raw := c.marshal()

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

func (r *RedisRunner) newConfig(name WorkerName, work Worker) (*WorkConfig, error) {
	wr, ok := r.RegistryWorkers[name]
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
