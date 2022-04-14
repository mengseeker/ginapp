package h

import (
	"ginapp/pkg/log"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var (
	logger = log.NewLogger(
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	)
)

func Debugf(c *gin.Context, template string, args ...interface{}) {
	logger.With(loggerFields(c)...).Debugf(template, args...)
}

func Debug(c *gin.Context, args ...interface{}) {
	logger.With(loggerFields(c)...).Debug(args...)
}

func Infof(c *gin.Context, template string, args ...interface{}) {
	logger.With(loggerFields(c)...).Infof(template, args...)
}
func Info(c *gin.Context, args ...interface{}) {
	logger.With(loggerFields(c)...).Info(args...)
}

func Errorf(c *gin.Context, template string, args ...interface{}) {
	logger.With(loggerFields(c)...).Errorf(template, args...)
}
func Error(c *gin.Context, args ...interface{}) {
	logger.With(loggerFields(c)...).Error(args...)
}

func Warnf(c *gin.Context, template string, args ...interface{}) {
	logger.With(loggerFields(c)...).Warnf(template, args...)
}
func Warn(c *gin.Context, args ...interface{}) {
	logger.With(loggerFields(c)...).Warn(args...)
}

func loggerFields(c *gin.Context) []interface{} {
	return []interface{}{
		zap.String("requestID", GetRequestID(c)),
	}
}
