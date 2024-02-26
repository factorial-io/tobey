package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetEnvString(t *testing.T) {
	// SET_STRING_GETENV must have been set before
	value := GetEnvString("SET_STRING_GETENV", "default-string-value")
	value2 := GetEnvString("NOT_SET_STRING_GETENV", "default-string-value")

	assert.Equal(t, value, "env-has-set")
	assert.Equal(t, value2, "default-string-value")
}

func TestGetEnvBool(t *testing.T) {
	// SET_STRING_GETENV must have been set before
	value, _ := GetEnvBool("SET_BOOL_GETENV", false)
	value2, _ := GetEnvBool("NOT_SET_BOOL_GETENV", false)

	assert.Equal(t, value, true)
	assert.Equal(t, value2, false)
}

func TestGetEnvInt(t *testing.T) {
	// SET_STRING_GETENV must have been set before
	value, _ := GetEnvInt("SET_INT_GETENV", 10)
	value2, _ := GetEnvInt("NOT_SET_INT_GETENV", 10)

	assert.Equal(t, value, 20)
	assert.Equal(t, value2, 10)
}

func TestGetEnvFloat(t *testing.T) {
	// SET_STRING_GETENV must have been set before
	value, _ := GetEnvFloat("SET_FLOAT_GETENV", 0.64)
	value2, _ := GetEnvFloat("NOT_SET_FLOAT_GETENV", 0.64)

	assert.Equal(t, value, 0.32)
	assert.Equal(t, value2, 0.64)
}
