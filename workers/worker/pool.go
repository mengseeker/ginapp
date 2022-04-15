package worker

import (
	"context"
	"time"

	"github.com/gomodule/redigo/redis"
)

var (
	redisCli redis.Conn
)

func Initialize(redisUrl string, opts ...redis.DialOption) error {
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	redisCli, err = redis.DialURLContext(ctx, redisUrl, opts...)
	return err
}

func postToRedis(c *WorkConfig) error {
	// raw := c.marshal()

	return nil
}

// Declare should used before worker Registry
func Declare(name WorkerName, work Worker, opts ...WorkerOption) (*WorkConfig, error) {
	c, err := new(name, work)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, postToRedis(c)
}
