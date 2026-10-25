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
type LogBackend struct {
	Host *XBMCHost
}

// Log ...
func (h *XBMCHost) Log(args ...any) {
	h.executeJSONRPCEx("Log", nil, args)
}

// NewLogBackend ...
func (h *XBMCHost) NewLogBackend() *LogBackend {
	return &LogBackend{h}
}

// GetKodiLog is returning kodi.log, read by python part
func (h *XBMCHost) GetKodiLog() []byte {
	retVal := []byte{}
	h.executeJSONRPCEx("GetKodiLog", &retVal, nil)
	return retVal
}

// Log ...
func (b *LogBackend) Log(level logging.Level, calldepth int, rec *logging.Record) error {
	line := rec.Formatted(calldepth + 1)
	switch level {
	case logging.CRITICAL:
		b.Host.Log(line, LogSevere)
	case logging.ERROR:
		b.Host.Log(line, LogError)
	case logging.WARNING:
		b.Host.Log(line, LogWarning)
	case logging.NOTICE:
		b.Host.Log(line, LogNotice)
	case logging.INFO:
		b.Host.Log(line, LogInfo)
	case logging.DEBUG:
		b.Host.Log(line, LogDebug)
	default:
	}
	return nil
}
