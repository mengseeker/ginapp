package log

import "go.uber.org/zap"

type ExtendLogger struct {
	zap.SugaredLogger
}

func (l *ExtendLogger) Printf(format string, args ...interface{}) {
	l.Infof(format, args...)
}
