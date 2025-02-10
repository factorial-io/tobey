// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "context"

// NoopProgressReporter is a no-op implementation of the ProgressReporter interface, it
// is used as the default when no progress reporting is configured.
type NoopProgressReporter struct{}

func (p *NoopProgressReporter) With(run *Run, url string) *Progress {
	return &Progress{
		reporter: p,
		Run:      run,
		URL:      url,
	}
}

func (p *NoopProgressReporter) Call(ctx context.Context, pu ProgressUpdate) error {
	return nil
}
