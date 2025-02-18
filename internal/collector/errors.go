// Copyright 2018 Adam Tauber. All rights reserved.
// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Based on the colly HTTP scraping framework by Adam Tauber, originally
// licensed under the Apache License 2.0 modified by Factorial GmbH.

package collector

import (
	"errors"
)

var (
	ErrCheckInternal = errors.New("internal check error")

	// ErrForbiddenDomain is the error thrown if visiting
	// a domain which is not allowed in AllowedDomains
	ErrForbiddenDomain = errors.New("forbidden domain")
	ErrForbiddenPath   = errors.New("forbidden path")

	// ErrMissingURL is the error type for missing URL errors
	ErrMissingURL = errors.New("missing URL")
	// ErrMaxDepth is the error type for exceeding max depth
	ErrMaxDepth = errors.New("max depth limit reached")
	// ErrForbiddenURL is the error thrown if visiting
	// a URL which is not allowed by URLFilters
	ErrForbiddenURL = errors.New("forbidden URL")

	// ErrRobotsTxtBlocked is the error type for robots.txt errors
	ErrRobotsTxtBlocked = errors.New("URL blocked by robots.txt")
	// ErrEmptyProxyURL is the error type for empty Proxy URL list
	ErrEmptyProxyURL = errors.New("proxy URL list is empty")
	// ErrAbortedAfterHeaders is the error returned when OnResponseHeaders aborts the transfer.
	ErrAbortedAfterHeaders = errors.New("aborted after receiving response headers")
)
