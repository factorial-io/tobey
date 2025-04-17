package main

import (
	"flag"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// configure parses env vars and flags, and sets global configuration accordingly.
func configure() (flagDaemon bool, req ConsoleRequest) {
	// basic / both
	flag.BoolVar(&flagDaemon, "d", false, "Run tobey as a service")
	var flagDebug bool
	var flagSkipCache bool
	var flagWorkers int
	var flagUserAgent string

	flag.BoolVar(&flagDebug, "debug", Debug, "Enable debug mode")
	flag.BoolVar(&flagSkipCache, "no-cache", false, "Disable caching")
	flag.IntVar(&flagWorkers, "w", NumVisitWorkers, "Number of concurrent workers to start")
	flag.StringVar(&flagUserAgent, "ua", UserAgent, "User Agent to use")

	// service only
	var flagHost string
	var flagPort int
	var flagDynamicConfig bool
	var flagTelemetry string

	flag.StringVar(&flagHost, "host", ListenHost, "Host interface to bind the HTTP server to (service only)")
	flag.IntVar(&flagPort, "port", ListenPort, "Port to bind the HTTP server to (service only)")
	flag.BoolVar(&flagDynamicConfig, "dynamic-config", DynamicConfig, "Enable dynamic configuration (service only)")
	flag.StringVar(&flagTelemetry, "telemetry", "", "Comma separated list of telemetry to enable: metrics, traces, pulse (service only)")

	// cli only
	var flagURLs string
	var flagOutputDir string
	var flagIgnores string
	var flagOutputContentOnly bool

	flag.StringVar(&flagURLs, "u", "", "Comma separated list of URls to crawl (cli only)")
	flag.StringVar(&flagOutputDir, "o", ".", "Directory to store results (cli only, defaults to current directory)")
	flag.StringVar(&flagIgnores, "i", "", "Comma separated list of paths to ignore (cli only)")
	flag.BoolVar(&flagOutputContentOnly, "oc", false, "Store response bodies directly on disk without JSON wrapper (cli only)")

	// parse
	flag.Parse()

	if isFlagPassed("debug") {
		Debug = flagDebug
	} else {
		Debug = isForgivingTrue(os.Getenv("TOBEY_DEBUG"))
	}
	if Debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
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

	if isFlagPassed("w") {
		NumVisitWorkers = flagWorkers
	} else if v := os.Getenv("TOBEY_WORKERS"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			panic(err)
		}
		NumVisitWorkers = p
	}

	if v := os.Getenv("TOBEY_USER_AGENT"); v != "" {
		UserAgent = v
	}
	if isFlagPassed("ua") {
		if flagDaemon {
			req.UserAgent = flagUserAgent
		} else {
			UserAgent = flagUserAgent // Potentially overwrite again.
		}
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

	if isFlagPassed("u") {
		req.URLs = strings.Split(flagURLs, ",")
	}

	if isFlagPassed("o") {
		req.OutputDir = flagOutputDir
	}

	if isFlagPassed("i") {
		req.IgnorePaths = strings.Split(flagIgnores, ",")
	}

	if isFlagPassed("oc") {
		req.OutputContentOnly = flagOutputContentOnly
	}

	return
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
