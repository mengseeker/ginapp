package h

import "github.com/gin-gonic/gin"

const (
	RequestIDHeaderKey = "X-Request-Id"
)

func GetRequestID(c *gin.Context) string {
	if id, exist := c.Get(RequestIDHeaderKey); exist {
		return id.(string)
	}
	return ""
}
