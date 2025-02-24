// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"tobey/internal/collector"
)

type DiskResultReporterConfig struct {
	OutputDir string `json:"output_dir"`
}

func newDiskResultReporterConfigFromDSN(dsn string) (DiskResultReporterConfig, error) {
	config := DiskResultReporterConfig{}

	u, err := url.Parse(dsn)
	if err != nil {
		return config, fmt.Errorf("invalid disk result reporter DSN: %w", err)
	}

	// FIXME: No windows support yet, would need to remove leading slash.
	config.OutputDir = u.Path

	return config, nil
}

// DiskResultReporter stores results on disk as JSON files. Results are grouped by run
// in a run specific directory. The directory structure is as follows:
//
//	<output_dir>/
//		<run_uuid>/
//			<url_hash>.json
//
// The <url_hash> is the SHA-256 hash of the request URL, encoded as a hex string.
// The JSON file contains the result as a JSON object.
type DiskResultReporter struct {
	outputDir string
}

type DiskResult struct {
	Run                string      `json:"run_uuid"`
	RunMetadata        interface{} `json:"run_metadata,omitempty"`
	RequestURL         string      `json:"request_url"`
	ResponseBody       []byte      `json:"response_body"` // Will be base64 encoded when JSON marshalled.
	ResponseStatusCode int         `json:"response_status_code"`
}

func NewDiskResultReporter(config DiskResultReporterConfig) (*DiskResultReporter, error) {
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	return &DiskResultReporter{
		outputDir: config.OutputDir,
	}, nil
}

// Accept implements ResultStore.Accept by writing results to a file.
//
// We accept per-call config in the signature, to satisfy the ResultReporter interface,
// but we don't use it here, as we don't allow dynamic config for this reporter.
func (drs *DiskResultReporter) Accept(ctx context.Context, config any, run *Run, res *collector.Response) error {
	logger := slog.With("run", run.ID, "url", res.Request.URL)
	logger.Debug("Result reporter: Saving result to file...")

	result := &DiskResult{
		Run:                run.ID,
		RequestURL:         res.Request.URL.String(),
		ResponseBody:       res.Body[:],
		ResponseStatusCode: res.StatusCode,
	}

	// MkdirAll ignores errors where the directory exists.
	runDir := filepath.Join(drs.outputDir, run.ID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("failed to create run directory: %w", err)
	}

	hash := sha256.New()
	hash.Write([]byte(res.Request.URL.String()))
	filename := fmt.Sprintf("%s.json", hex.EncodeToString(hash.Sum(nil)))
	filepath := filepath.Join(runDir, filename)

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := os.WriteFile(filepath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
