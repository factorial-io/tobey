// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package progress

import (
	"context"
	"fmt"
	"log/slog"
)

// ConsoleReporter outputs progress updates to the console.
type ConsoleReporter struct{}

func (c *ConsoleReporter) With(runID string, url string) *Progress {
	return &Progress{
		reporter: c,
		Stage:    "initial",
		RunID:    runID,
		URL:      url,
	}
}

// Call outputs the progress update to the console.
func (c *ConsoleReporter) Call(ctx context.Context, pu Update) error {
	slog.Info(fmt.Sprintf("Progress Update: -> %d", pu.Status), "run", pu.RunID, "url", pu.URL)
	return nil
}
