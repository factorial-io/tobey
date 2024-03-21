// Copyright 2024 Factorial GmbH. All rights reserved.

package collector

func normalizeURL(u string) string {
	parsed, err := urlParser.Parse(u)
	if err != nil {
		return u
	}
	return parsed.String()
}
