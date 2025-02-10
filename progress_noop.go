// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "context"

type NoopProgressDispatcher struct {
}

func (p *NoopProgressDispatcher) With(run *Run, url string) *Progressor {
	return &Progressor{
		dispatcher: p,
		Run:        run,
		URL:        url,
	}
}

func (p *NoopProgressDispatcher) Call(ctx context.Context, pu ProgressUpdate) error {
	return nil
}
