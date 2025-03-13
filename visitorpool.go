// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"tobey/internal/ctrlq"
)

// NewVisitorPool initializes a worker pool and fills it with a number
// of VisitWorker.
func NewVisitorPool(
	ctx context.Context,
	num int,
	runs *RunManager,
	q ctrlq.VisitWorkQueue,
	rr ResultReporter,
	pr ProgressReporter,
) *VisitorPool {
	pool := &VisitorPool{
		startWorkersNum: num,
		workers:         make([]*Visitor, 0, num),
	}

	slog.Debug("Visitor: Starting workers...", "num", num)
	for i := 0; i < num; i++ {
		worker := &Visitor{
			id:       i,
			runs:     runs,
			queue:    q,
			result:   rr,
			progress: pr,
			logger:   slog.With("worker", i),
		}
		pool.workers = append(pool.workers, worker)
		pool.wg.Add(1)
		worker.Start(ctx, &pool.wg)
	}
	return pool
}

// VisitorPool manages a pool of visit workers
type VisitorPool struct {
	startWorkersNum int // The original number of workers that were started.
	workers         []*Visitor
	mu              sync.RWMutex
	wg              sync.WaitGroup
}

// Close waits for all workers to finish
func (p *VisitorPool) Close() {
	p.wg.Wait()
}

// IsHealthy checks whether the worker pool is functional.
func (p *VisitorPool) IsHealthy() (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.workers) < p.startWorkersNum {
		return false, fmt.Errorf("insufficient workers: %d/%d", len(p.workers), p.startWorkersNum)
	}
	return true, nil
}
