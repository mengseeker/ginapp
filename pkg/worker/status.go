package worker

import "time"

type RedisRunnerStatus struct {
	StartAt      time.Time
	PullCount    int
	ExecCount    int
	ExecErrCount int
}
