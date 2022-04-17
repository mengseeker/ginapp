package util

import (
	"time"

	"github.com/gomodule/redigo/redis"
)

func RedisLocker(cli redis.Conn, key string, val interface{}, ttl time.Duration, f func()) (bool, error) {
	reply, err := cli.Do("SET", key, val, "EX", int(ttl.Seconds()), "NX")
	if err != nil {
		return false, nil
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
