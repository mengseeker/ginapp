package v1

import "github.com/gin-gonic/gin"

func Panic(c *gin.Context) {
	panic("test panic")
}
