package v1

import (
	"ginapp/pkg/worker"
	"ginapp/workers"
	"time"

	"github.com/gin-gonic/gin"
)

func exampleWorker(c *gin.Context) {
	t := c.Query("t")
	var r *worker.WorkConfig
	var err error
	w := workers.ExampleWorker{}
	switch t {
	case "panic":
		w.Panic = true
		r, err = w.Declare()
	case "timeout":
		i := 30 * time.Second
		w.Timeout = &i
		r, err = w.Declare(worker.WithTimeout(time.Second))
	case "retry":
		w.Error = "retry"
		r, err = w.Declare(worker.WithRetry(3))
	case "err":
		w.Error = "test"
		r, err = w.Declare()
	case "delay":
		r, err = w.Declare(worker.WithPerformAt(time.Now().Add(10 * time.Second)))
	default:
		r, err = w.Declare()
	}
	c.JSON(200, gin.H{
		"worker": r,
		"err":    err,
	})
}
