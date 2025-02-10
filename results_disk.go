// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"tobey/internal/collector"
)

// DiskStoreConfig holds configuration for DiskResultsStore
type DiskStoreConfig struct {
	OutputDir string `json:"output_dir"`
}

// DiskResultStore implements ResultsStore by saving results to files on disk
type DiskResultStore struct {
	outputDir string
}

// NewDiskResultStore creates a new DiskResultStore
func NewDiskResultStore(config DiskStoreConfig) (*DiskResultStore, error) {
	// Create default output directory if it doesn't exist
	if config.OutputDir != "" {
		if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	return &DiskResultStore{
		outputDir: config.OutputDir,
	}, nil
}

// Save implements ResultStore.Save by writing results to a file.
//
// We accept per-call config in the signature, to satisfy the ResultsStore interface,
// but we don't use it here, as we don't allow dynamic config for this store.
func (drs *DiskResultStore) Save(ctx context.Context, config any, run *Run, res *collector.Response) error {
	logger := slog.With("run", run.ID, "url", res.Request.URL)
	logger.Debug("DiskResultStore: Saving result to file...")

	result := NewResult(run, res)

	// Create a filename based on URL and run ID
	urlPath := sanitizeFilename(res.Request.URL.Path)
	if urlPath == "" {
		urlPath = "root"
	}

	filename := fmt.Sprintf("%s_%s.json", run.ID, urlPath)
	filepath := filepath.Join(drs.outputDir, filename)

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Error("DiskResultsStore: Failed to marshal result", "error", err)
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	if err := os.WriteFile(filepath, jsonData, 0644); err != nil {
		logger.Error("DiskResultsStore: Failed to write file", "error", err, "path", filepath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	logger.Debug("DiskResultsStore: Successfully saved result", "path", filepath)
	return nil
}

// sanitizeFilename creates a safe filename from a URL path
func sanitizeFilename(path string) string {
	// Remove leading slash
	path = filepath.Clean(path)
	if path == "/" || path == "." {
		return ""
	}
	if path[0] == '/' {
		path = path[1:]
	}

	// Replace remaining slashes with underscores
	path = filepath.ToSlash(path)
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			path = path[:i] + "_" + path[i+1:]
		}
	}

	// Limit filename length
	if len(path) > 100 {
		path = path[:100]
	}

	return path
}
