package main

import (
	"log/slog"
	"os"
	"strconv"
)

// GetEnvString gets the environment variable for a key and if that env-var hasn't been set it returns the default value
func GetEnvString(key string, defaultVal string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		value = defaultVal
	}

	slog.Debug("Set Environment ", "key", key, "value", value)

	return value
}

// GetEnvBool gets the environment variable for a key and if that env-var hasn't been set it returns the default value
func GetEnvBool(key string, defaultVal bool) bool {
	envvalue := os.Getenv(key)
	value, err := strconv.ParseBool(envvalue)
	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	slog.Debug("Set Environment ", "key", key, "value", value)

	return value
}

// GetEnvInt gets the environment variable for a key and if that env-var hasn't been set it returns the default value. This function is equivalent to ParseInt(s, 10, 0) to convert env-vars to type int
func GetEnvInt(key string, defaultVal int) int {
	envvalue := os.Getenv(key)
	value, err := strconv.Atoi(envvalue)

	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	slog.Debug("Set Environment ", "key", key, "value", value)

	return value
}

// GetEnvFloat gets the environment variable for a key and if that env-var hasn't been set it returns the default value. This function uses bitSize of 64 to convert string to float64.
func GetEnvFloat(key string, defaultVal float64) float64 {
	envvalue := os.Getenv(key)
	value, err := strconv.ParseFloat(envvalue, 64)
	if len(envvalue) == 0 || err != nil {
		value := defaultVal
		return value
	}

	slog.Debug("Set Environment ", "key", key, "value", value)

	return value
}
