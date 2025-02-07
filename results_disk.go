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

func (c *DiskStoreConfig) Validate() error {
	// No validation needed for now, but we could add checks for write permissions, etc.
	return nil
}

// DiskResultStore implements ResultsStore by saving results to files on disk
type DiskResultStore struct {
	defaultConfig DiskStoreConfig
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
		defaultConfig: config,
	}, nil
}

// Save implements ResultStore.Save by writing results to a file
func (drs *DiskResultStore) Save(ctx context.Context, config ResultStoreConfig, run string, res *collector.Response) error {
	logger := slog.With("run", run, "url", res.Request.URL)
	logger.Debug("DiskResultStore: Saving result to file...")

	// Use per-call config if provided, otherwise use default config
	outputDir := drs.defaultConfig.OutputDir
	var webhookData interface{}

	if config != nil {
		if diskConfig, ok := config.(*DiskStoreConfig); ok {
			if diskConfig.OutputDir != "" {
				outputDir = diskConfig.OutputDir
				// Create directory if it doesn't exist
				if err := os.MkdirAll(outputDir, 0755); err != nil {
					return fmt.Errorf("failed to create output directory: %w", err)
				}
			}
		}
		if whConfig, ok := config.(*WebhookResultStoreConfig); ok {
			webhookData = whConfig.Data
		}
	}

	// Create result using common Result type
	result := NewResult(run, res, webhookData)

	// Create a filename based on URL and run ID
	urlPath := sanitizeFilename(res.Request.URL.Path)
	if urlPath == "" {
		urlPath = "root"
	}

	filename := fmt.Sprintf("%s_%s.json", run, urlPath)
	filepath := filepath.Join(outputDir, filename)

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
