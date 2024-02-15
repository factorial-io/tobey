package helper

import (
	"os"
	"strconv"
	logger "tobey/logger"
)

// GetEnvString gets the environment variable for a key and if that env-var hasn't been set it returns the default value
func GetEnvString(key string, defaultVal string) string {
	log := logger.GetBaseLogger()
	value := os.Getenv(key)
	if len(value) == 0 {
		value = defaultVal
	}

	log.Trace("Set Environment", key, " to ", value, ".")

	return value
}

// GetEnvBool gets the environment variable for a key and if that env-var hasn't been set it returns the default value
func GetEnvBool(key string, defaultVal bool) bool {
	log := logger.GetBaseLogger()
	envvalue := os.Getenv(key)
	value, err := strconv.ParseBool(envvalue)
	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	log.Trace("Set Environment", key, " to ", value, ".")

	return value
}

// GetEnvInt gets the environment variable for a key and if that env-var hasn't been set it returns the default value. This function is equivalent to ParseInt(s, 10, 0) to convert env-vars to type int
func GetEnvInt(key string, defaultVal int) int {
	log := logger.GetBaseLogger()
	envvalue := os.Getenv(key)
	value, err := strconv.Atoi(envvalue)

	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	log.Trace("Set Environment", key, " to ", value, ".")

	return value
}

// GetEnvFloat gets the environment variable for a key and if that env-var hasn't been set it returns the default value. This function uses bitSize of 64 to convert string to float64.
func GetEnvFloat(key string, defaultVal float64) float64 {
	log := logger.GetBaseLogger()
	envvalue := os.Getenv(key)
	value, err := strconv.ParseFloat(envvalue, 64)
	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	log.Trace("Set Environment", key, " to ", value, ".")

	return value
}
