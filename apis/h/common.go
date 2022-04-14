package h

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	LogSkipPaths = map[string]bool{
		"/health":  true,
		"/metrics": true,
	}
)

func MidRecovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err interface{}) {
		// 和AbortDirect配合，支持通过panic方式直接返回错误
		if e, ok := err.(Response); ok {
			c.JSON(http.StatusOK, e)
			return
		}
		R(c, CodeServerError, "ServerError", nil)
		logger.With(loggerFields(c)...).With(zap.Stack("stacks")).Errorf("panic: %v", err)
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
