package main

import (
	"flag"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

func configure() {
	// Add command line flag parsing
	var flagHost string
	var flagPort int

	flag.StringVar(&flagHost, "host", "", "Host interface to bind the HTTP server to")
	flag.IntVar(&flagPort, "port", 0, "Port to bind the HTTP server to")
	flag.Parse()

	if isForgivingTrue(os.Getenv("TOBEY_DEBUG")) {
		Debug = true
		slog.Info("Debug mode enabled.")
	}
	if isForgivingTrue(os.Getenv("TOBEY_SKIP_CACHE")) {
		SkipCache = true
		slog.Info("Skipping cache.")
	}

	// First check command line args, then fall back to env vars
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

	if v := os.Getenv("TOBEY_WORKERS"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			panic(err)
		}
		NumVisitWorkers = p
	}
	slog.Info("Number of visit workers configured.", "num", NumVisitWorkers)

	if v := os.Getenv("TOBEY_USER_AGENT"); v != "" {
		UserAgent = v
		slog.Info("Using custom user agent.", "user_agent", UserAgent)
	}

	if isForgivingTrue(os.Getenv("TOBEY_DYNAMIC_CONFIG")) {
		DynamicConfig = true
		slog.Info("Dynamic configuration enabled!")
	}

	v := os.Getenv("TOBEY_TELEMETRY")
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
