// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ctrlq

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/propagation"
)

type VisitMessage struct {
	ID  uint32
	Run string

	URL string

	Created time.Time

	// The number of times this job has been retried to be enqueued.
	Retries uint32

	// The carrier is used to pass tracing information from the job publisher to
	// the job consumer. It is used to pass the TraceID and SpanID.
	Carrier propagation.MapCarrier
}

// VisitJob is similar to a http.Request, it exists only for a certain time. It
// carries its own Context. And although this violates the strict variant of the
// "do not store context on struct" it doe not violate the relaxed "do not store
// a context" rule, as a Job is transitive.
//
// We initially saw the requirement to pass a context here as we wanted to carry
// over TraceID and SpanID from the job publisher.
type VisitJob struct {
	*VisitMessage
	Context context.Context
}

// Validate ensures mandatory fields are non-zero.
func (j *VisitJob) Validate() (bool, error) {
	if j.Run == "" {
		return false, errors.New("job without run")
	}
	if j.URL == "" {
		return false, errors.New("job without URL")
	}
	return true, nil
}
