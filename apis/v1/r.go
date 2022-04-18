package v1

import "github.com/gin-gonic/gin"

func Mount(g *gin.RouterGroup) {
	g.GET("helloworld", helloworld)
	g.GET("abort", abort)
	g.GET("worker", exampleWorker)
}
