package util

import (
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
)

func RedisLocker(cli redis.Conn, key, val string, ttl time.Duration, f func()) (bool, error) {
	reply, err := cli.Do("SET", key, val, "EX", int(ttl.Seconds()), "NX")
	if err != nil {
		return false, fmt.Errorf("get locker %s, err: %v", key, err)
	}
	if reply != nil {
		defer func() {
			cli.Do("DEL", key)
		}()
		f()
		return true, nil
	}
	return false, nil
}
