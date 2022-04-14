package log

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger = zap.SugaredLogger

var level = zap.InfoLevel

func init() {
	if os.Getenv("DEBUG") == "1" {
		level = zap.DebugLevel
	}
}

func NewLogger(opts ...zap.Option) *Logger {
	c := zap.NewProductionConfig()
	// c := zap.NewDevelopmentConfig()
	c.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	c.Level = zap.NewAtomicLevelAt(level)
	c.Encoding = "console"
	c.DisableStacktrace = true
	l, err := c.Build(opts...)
	if err != nil {
		panic(err)
	}
	return l.Sugar()
}
