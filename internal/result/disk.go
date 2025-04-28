// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"tobey/internal/collector"
)

type diskResult struct {
	Run                string      `json:"run"`
	RunMetadata        interface{} `json:"run_metadata,omitempty"`
	RequestURL         string      `json:"request_url"`
	ResponseBody       []byte      `json:"response_body"` // Will be base64 encoded when JSON marshalled.
	ResponseStatusCode int         `json:"response_status_code"`
}

type DiskConfig struct {
	OutputDir         string `json:"output_dir"`
	OutputContentOnly bool   `json:"output_content_only"`
}

// LogFormat returns a string representation of the config suitable for logging
func (c DiskConfig) LogFormat() string {
	return fmt.Sprintf("disk(output_dir=%s, output_content_only=%v)", c.OutputDir, c.OutputContentOnly)
}

func NewDiskConfigFromDSN(dsn string, constrainPaths bool) (DiskConfig, error) {
	config := DiskConfig{}

	u, err := url.Parse(dsn)
	if err != nil {
		return config, fmt.Errorf("invalid disk result reporter DSN: %w", err)
	}

	// Ensure the output directory is below the current working directory. It must
	// not be above the current working directory, to prevent directory traversal
	// attacks. Allow absolute paths, as long as they resolve to a directory above
	// the current working directory.
	wd, err := os.Getwd()
	if err != nil {
		return config, fmt.Errorf("invalid disk result reporter DSN: %w", err)
	}

	// If a relative single path element is provided it is parsed as the host.
	var p string
	if u.Path == "" && u.Host != "" {
		p = u.Host
	} else {
		p = u.Path
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return config, fmt.Errorf("invalid disk result reporter DSN: %w", err)
	}

	// Only enforce path constraints when dynamic configuration is enabled.
	if constrainPaths && !strings.HasPrefix(abs, wd) {
		return config, fmt.Errorf("output directory (%s) must be below the current working directory (%s)", abs, wd)
	}

	// FIXME: No windows support yet, would need to remove leading slash.
	config.OutputDir = abs

	return config, nil
}

// ReportToDisk stores results on disk as JSON files. Results are grouped by run
// in a run specific directory. The directory structure is as follows:
//
//	<output_dir>/
//		<run_uuid>/
//			<url_hash>.json
//
// The <url_hash> is the SHA-256 hash of the request URL, encoded as a hex string.
// The JSON file contains the result as a JSON object.
func ReportToDisk(ctx context.Context, config DiskConfig, runID string, res *collector.Response) error {
	logger := slog.With("run", runID, "url", res.Request.URL)

	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return err
	}
	logger.Debug("Result reporter: Saving result to file...")

	hash := sha256.New()
	hash.Write([]byte(res.Request.URL.String()))
	filename := hex.EncodeToString(hash.Sum(nil))

	runDir := filepath.Join(config.OutputDir, runID)
	filepath := filepath.Join(runDir, filename)

	// MkdirAll ignores errors where the directory exists, so we don't need to check for it.
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return err
	}

	if config.OutputContentOnly {
		// Get the Content-Type from the response headers
		contentType := res.Headers.Get("Content-Type")
		if contentType != "" {
			// Try to get extension from MIME type
			exts, err := mime.ExtensionsByType(contentType)
			if err == nil && len(exts) > 0 {
				filepath = filepath + exts[0]
			}
		}

		// Store only the response body directly
		return os.WriteFile(filepath, res.Body, 0644)
	}

	// Store as JSON with metadata
	result := &diskResult{
		Run:                runID,
		RequestURL:         res.Request.URL.String(),
		ResponseBody:       res.Body[:],
		ResponseStatusCode: res.StatusCode,
	}

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath+".json", jsonData, 0644)
}
