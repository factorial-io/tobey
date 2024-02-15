package Logger

import (
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

func InitLoggerDefault(log_level string) {

	// Log as JSON instead of the default ASCII formatter.
	log.SetFormatter(&log.JSONFormatter{})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	log.SetLevel(getLevelByString(log_level))
}

func GetBaseLogger() *log.Entry {
	requestLogger := log.WithField("service", "torbey")
	return requestLogger
}

func getLevelByString(level string) log.Level {
	switch strings.ToLower(level) {
	case "trace":
		return log.TraceLevel
	case "debug":
		return log.DebugLevel
	case "info", "notice":
		return log.InfoLevel
	case "warning", "warn":
		return log.WarnLevel
	case "error":
		return log.ErrorLevel
	case "critical", "alert":
		return log.FatalLevel
	case "panic", "emergency":
		return log.PanicLevel
	default:
		return log.TraceLevel
	}
}
