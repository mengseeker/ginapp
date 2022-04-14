package v1

import (
	"ginapp/apis/h"
	"time"

	"github.com/gin-gonic/gin"
)

func abort(c *gin.Context) {
	t := c.Query("t")
	switch t {
	case "needLogin":
		h.AbortDirect(c, "needLogin")
	case "vals":
		h.AbortDirect(c, "vals", 233, time.Now().GoString())
	case "panic":
		panic("test panic")
	default:
		h.AbortDirect(c, "unknow")
	}
}
