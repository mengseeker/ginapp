package v1

import (
	"ginapp/apis/h"
	"ginapp/pkg/worker"
	"ginapp/workers"
	"time"

	"github.com/gin-gonic/gin"
)

func exampleWorker(c *gin.Context) {
	t := c.Query("t")
	var err error
	w := &workers.ExampleWorker{}
	var r string
	h.Infof(c, "create worker type: %s", t)
	switch t {
	case "panic":
		w.Panic = true
		r, err = workers.DeclareWorker(w)
	case "timeout":
		i := 30 * time.Second
		w.Timeout = &i
		r, err = workers.DeclareWorker(w, worker.WithTimeout(time.Second))
	case "retry":
		w.Error = "retry"
		r, err = workers.DeclareWorker(w, worker.WithRetry(3))
	case "err":
		w.Error = "test"
		r, err = workers.DeclareWorker(w)
	case "delay":
		r, err = workers.DeclareWorker(w, worker.WithPerformAt(time.Now().Add(10*time.Second)))
	case "bench":
		for i := 0; i < 100000; i++ {
			workers.DeclareWorker(w, worker.WithPerformAt(time.Now().Add(10*time.Second)))
		}
	default:
		r, err = workers.DeclareWorker(w)
	}
	c.JSON(200, gin.H{
		"worker": r,
		"err":    err,
	})
}
