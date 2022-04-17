package worker

import "time"

type WorkerOption func(c *WorkConfig)

// retry < 0 means retry count unlimits
// default WorkerDefaultRetryCount
func WithRetry(retry int) WorkerOption {
	return func(c *WorkConfig) {
		c.Retry = retry
	}
}

// default WorkerDefaultTimeout
func WithTimeout(timeout time.Duration) WorkerOption {
	return func(c *WorkConfig) {
		c.Timeout = timeout
	}
}

// set worker execute time
func WithPerformAt(performAt time.Time) WorkerOption {
	return func(c *WorkConfig) {
		c.PerformAt = &performAt
	}
}

// set worker Queue
// default low
func WithQueue(q WorkerQueue) WorkerOption {
	return func(c *WorkConfig) {
		c.Queue = q
	}
}
