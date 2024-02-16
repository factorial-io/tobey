// Based on colly HTTP scraping framework, Copyright 2018 Adam Tauber,
// originally licensed under the Apache License 2.0

package collector

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kennygrant/sanitize"
)

// SanitizeFileName replaces dangerous characters in a string
// so the return value can be used as a safe file name.
func SanitizeFileName(fileName string) string {
	ext := filepath.Ext(fileName)
	cleanExt := sanitize.BaseName(ext)
	if cleanExt == "" {
		cleanExt = ".unknown"
	}
	return strings.Replace(fmt.Sprintf(
		"%s.%s",
		sanitize.BaseName(fileName[:len(fileName)-len(ext)]),
		cleanExt[1:],
	), "-", "_", -1)
}
