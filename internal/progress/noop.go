// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package progress

import "context"

// NoopReporter is a no-op implementation of the Reporter interface, it
// is used as the default when no progress reporting is configured.
type NoopReporter struct{}

func (p *NoopReporter) With(runID string, url string) *Progress {
	return &Progress{
		reporter: p,
		RunID:    runID,
		URL:      url,
	}
}

func (p *NoopReporter) Call(ctx context.Context, pu Update) error {
	return nil
}
