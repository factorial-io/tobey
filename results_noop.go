// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"tobey/internal/collector"
)

func ReportResultToNoop(ctx context.Context, config any, run *Run, res *collector.Response) error {
	return nil
}
