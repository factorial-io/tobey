package main

import (
	"flag"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

func configure() {
	var flagHost string
	var flagPort int
	var flagDebug bool
	var flagSkipCache bool
	var flagWorkers int
	var flagUserAgent string
	var flagDynamicConfig bool
	var flagTelemetry string

	flag.StringVar(&flagHost, "host", ListenHost, "Host interface to bind the HTTP server to")
	flag.IntVar(&flagPort, "port", ListenPort, "Port to bind the HTTP server to")
	flag.BoolVar(&flagDebug, "debug", Debug, "Enable debug mode")
	flag.BoolVar(&flagSkipCache, "no-cache", false, "Disable caching")
	flag.IntVar(&flagWorkers, "workers", NumVisitWorkers, "Number of wokers to start")
	flag.StringVar(&flagUserAgent, "ua", UserAgent, "User Agent to use")
	flag.BoolVar(&flagDynamicConfig, "dynamic-config", DynamicConfig, "Enable dynamic configuration")
	flag.StringVar(&flagTelemetry, "telemetry", "", "Comma separated list of telemetry to enable: metrics, traces, pulse")
	flag.Parse()

	if isFlagPassed("debug") {
		Debug = flagDebug
	} else {
		Debug = isForgivingTrue(os.Getenv("TOBEY_DEBUG"))
	}
	if Debug {
		slog.Info("Debug mode enabled!")
	}

	if isFlagPassed("no-cache") {
		SkipCache = flagSkipCache
	} else {
		SkipCache = isForgivingTrue(os.Getenv("TOBEY_SKIP_CACHE"))
	}
	if SkipCache {
		slog.Info("Skipping cache!")
	}

	if flagHost != "" {
		ListenHost = flagHost
	} else if v := os.Getenv("TOBEY_HOST"); v != "" {
		ListenHost = v
	}
	if flagPort != 0 {
		ListenPort = flagPort
	} else if v := os.Getenv("TOBEY_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			panic(err)
		}
		ListenPort = p
	}

	if isFlagPassed("workers") {
		NumVisitWorkers = flagWorkers
	} else if v := os.Getenv("TOBEY_WORKERS"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			panic(err)
		}
		NumVisitWorkers = p
	}

	if isFlagPassed("ua") {
		UserAgent = flagUserAgent
	} else if v := os.Getenv("TOBEY_USER_AGENT"); v != "" {
		UserAgent = v
	}

	if isFlagPassed("dynamic-config") {
		DynamicConfig = flagDynamicConfig
	} else {
		DynamicConfig = isForgivingTrue(os.Getenv("TOBEY_DYNAMIC_CONFIG"))
	}
	if DynamicConfig {
		slog.Warn("Dynamic configuration enabled!")
	}

	var v string
	if isFlagPassed("telemetry") {
		v = flagTelemetry
	} else {
		v = os.Getenv("TOBEY_TELEMETRY")
	}
	if strings.Contains(v, "traces") || strings.Contains(v, "tracing") {
		UseTracing = true
		slog.Info("Tracing enabled.")
	}
	if strings.Contains(v, "metrics") {
		UseMetrics = true
		slog.Info("Metrics enabled.")
	}
	if strings.Contains(v, "pulse") {
		UsePulse = true
		slog.Info("High Frequency Metrics (Pulse) enabled.")
	}
}

func isForgivingTrue(v string) bool {
	v = strings.ToLower(v)
	return v == "true" || v == "yes" || v == "y" || v == "on"
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
