package v1

import (
	"ginapp/apis/h"

	"github.com/gin-gonic/gin"
)

func helloworld(c *gin.Context) {
	h.Infof(c, "helloworld %s", c.Query("l"))
	h.RR(c, "helloworld")
}
