package worker

import (
	"time"

	"github.com/google/uuid"
)

type Queue = string

const (
	QueueHigh Queue = "High"
	QueueLow  Queue = "Low"
)

type Meta struct {
	ID        string
	Name      string
	Timeout   time.Duration
	PerformAt *time.Time
	Retry     int
	Queue     Queue

	CreatedAt  time.Time
	RetryCount int
	Success    bool
	Error      string
	Raw        []byte
}

func NewMetaByWorker(w Worker, opts ...Option) (*Meta, error) {
	m := Meta{
		ID: uuid.NewString(),
		// Name:      name,
		// WorkerRaw: raw,
		// Retry:     WorkerDefaultRetryCount,
		// Timeout:   WorkerDefaultTimeout,
		// Queue:     QueueLow,

		CreatedAt: time.Now(),
	}
	for _, opt := range opts {
		opt(&m)
	}
	return &m, nil
}

type Option func(c *Meta)

// retry < 0 means retry count unlimits
// default WorkerDefaultRetryCount
func WithRetry(retry int) Option {
	return func(c *Meta) {
		c.Retry = retry
	}
}

// default WorkerDefaultTimeout
func WithTimeout(timeout time.Duration) Option {
	return func(c *Meta) {
		c.Timeout = timeout
	}
}

// set worker execute time
func WithPerformAt(performAt time.Time) Option {
	return func(c *Meta) {
		c.PerformAt = &performAt
	}
}

// set worker Queue
// default low
func WithQueue(q Queue) Option {
	return func(c *Meta) {
		c.Queue = q
	}
}
