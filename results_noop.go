// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"tobey/internal/collector"
)

// NoopResultStore implements ResultsStore but discards all results
type NoopResultStore struct{}

// Save implements ResultsStore.Save by discarding the result
func (n *NoopResultStore) Save(ctx context.Context, config ResultStoreConfig, run string, res *collector.Response) error {
	return nil
}
