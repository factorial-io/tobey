// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result

import (
	"context"
	"tobey/internal/collector"
)

type DiscoverySource string

const (
	DiscoverySourceSitemap DiscoverySource = "sitemap"
	DiscoverySourceRobots  DiscoverySource = "robots"
	DiscoverySourceLink    DiscoverySource = "link"
)

// Reporter is a function type that can be used to report the result of a crawl.
type Reporter func(ctx context.Context, runID string, res *collector.Response) error
