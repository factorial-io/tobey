// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result

import (
	"context"
	"tobey/internal/collector"
)

func ReportToNoop(ctx context.Context, config any, runID string, res *collector.Response) error {
	return nil
}
