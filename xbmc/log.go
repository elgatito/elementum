package xbmc

import (
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("xbmc")

const (
	// LogDebug ...
	LogDebug = iota
	// LogInfo ...
	LogInfo
	// LogNotice ...
	LogNotice
	// LogWarning ...
	LogWarning
	// LogError ...
	LogError
	// LogSevere ...
	LogSevere
	// LogFatal ...
	LogFatal
	// LogNone ...
	LogNone
)

// LogBackend ...
type LogBackend struct{}

// Log ...
func Log(args ...interface{}) {
	executeJSONRPCEx("Log", nil, args)
}

// NewLogBackend ...
func NewLogBackend() *LogBackend {
	return &LogBackend{}
}

// Log ...
func (b *LogBackend) Log(level logging.Level, calldepth int, rec *logging.Record) error {
	line := rec.Formatted(calldepth + 1)
	switch level {
	case logging.CRITICAL:
		Log(line, LogSevere)
	case logging.ERROR:
		Log(line, LogError)
	case logging.WARNING:
		Log(line, LogWarning)
	case logging.NOTICE:
		Log(line, LogNotice)
	case logging.INFO:
		Log(line, LogInfo)
	case logging.DEBUG:
		Log(line, LogDebug)
	default:
	}
	return nil
}
