package apis

import (
	"ginapp/apis/h"
	v1 "ginapp/apis/v1"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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

func Mount(g *gin.RouterGroup) {
	// global apis
	g.GET("/health", Health)
	g.GET("/metrics", Metrics)

	v1.Mount(g)
}

func Health(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

func Metrics(c *gin.Context) {
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
}
