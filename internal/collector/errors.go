// Based on colly HTTP scraping framework, Copyright 2018 Adam Tauber,
// originally licensed under the Apache License 2.0

package collector

import "errors"

var (
	// ErrForbiddenDomain is the error thrown if visiting
	// a domain which is not allowed in AllowedDomains
	ErrForbiddenDomain = errors.New("Forbidden domain")
	// ErrMissingURL is the error type for missing URL errors
	ErrMissingURL = errors.New("Missing URL")
	// ErrMaxDepth is the error type for exceeding max depth
	ErrMaxDepth = errors.New("Max depth limit reached")
	// ErrForbiddenURL is the error thrown if visiting
	// a URL which is not allowed by URLFilters
	ErrForbiddenURL = errors.New("ForbiddenURL")

	// ErrRobotsTxtBlocked is the error type for robots.txt errors
	ErrRobotsTxtBlocked = errors.New("URL blocked by robots.txt")
	// ErrEmptyProxyURL is the error type for empty Proxy URL list
	ErrEmptyProxyURL = errors.New("Proxy URL list is empty")
	// ErrAbortedAfterHeaders is the error returned when OnResponseHeaders aborts the transfer.
	ErrAbortedAfterHeaders = errors.New("Aborted after receiving response headers")
)
