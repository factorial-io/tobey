// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"tobey/internal/collector"
)

// NoopResultReporter implements ResultsStore but discards all results
type NoopResultReporter struct{}

// Accept implements ResultsStore.Accept by discarding the result
func (n *NoopResultReporter) Accept(ctx context.Context, config any, run *Run, res *collector.Response) error {
	return nil
}
