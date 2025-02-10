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
