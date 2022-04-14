package h

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	LogSkipPaths = map[string]bool{
		"/health":  true,
		"/metrics": true,
	}
)

func Health(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

func Metrics(c *gin.Context) {
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
}

func MidRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err interface{}) {
		R(c, CodeServerError, "ServerError", nil)
		// h.Error(c, err)
	})
}

func MidSetRequestID() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id := ctx.GetHeader(RequestIDHeaderKey)
		if id == "" {
			id = uuid.NewString()
		}
		ctx.Set(RequestIDHeaderKey, id)
		ctx.Next()
	}
}

func MidLogger() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// Start timer
		start := time.Now()
		path := ctx.Request.URL.Path

		// Process request
		ctx.Next()

		// Log only when path is not being skipped
		if ok := LogSkipPaths[path]; !ok {
			raw := ctx.Request.URL.Path
			if ctx.Request.URL.RawQuery != "" {
				raw = raw + "?" + ctx.Request.URL.RawQuery
			}
			Latency := time.Since(start)
			Infof(ctx, "%s %s %d %dus", ctx.Request.Method, raw, ctx.Writer.Status(), Latency.Microseconds())
		}
	}
}
