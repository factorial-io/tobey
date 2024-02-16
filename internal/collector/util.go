// Based on colly HTTP scraping framework, Copyright 2018 Adam Tauber,
// originally licensed under the Apache License 2.0

package collector

func normalizeURL(u string) string {
	parsed, err := urlParser.Parse(u)
	if err != nil {
		return u
	}
	return parsed.String()
}
