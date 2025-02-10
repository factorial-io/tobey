package main

import (
	"context"
	"fmt"
	"log/slog"
)

type ConsoleProgressReporter struct{}

func (c *ConsoleProgressReporter) With(run *Run, url string) *Progress {
	return &Progress{
		reporter: c,
		Stage:    "initial",
		Run:      run,
		URL:      url,
	}
}

// Call outputs the progress update to the console.
func (c *ConsoleProgressReporter) Call(ctx context.Context, pu ProgressUpdate) error {
	slog.Info(fmt.Sprintf("Progress Update: -> %d", pu.Status), "run", pu.Run.ShortID(), "url", pu.URL)
	return nil
}
