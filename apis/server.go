package apis

import (
	"ginapp/apis/h"
	v1 "ginapp/apis/v1"

	"github.com/gin-gonic/gin"
)

func Mount(g *gin.RouterGroup) {
	// global apis
	g.GET("/health", h.Health)
	g.GET("/metrics", h.Metrics)

	v1.Mount(g)
}

func Serve(addr string) error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// generate X-Request-Id
	router.Use(h.MidSetRequestID())

	// logger
	router.Use(h.MidLogger())

	// local recovery
	router.Use(h.MidRecovery())

	// mount routes
	Mount(&router.RouterGroup)

	return router.Run(addr)
}
