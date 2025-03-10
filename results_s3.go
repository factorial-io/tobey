// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"tobey/internal/collector"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Result struct {
	Run                string      `json:"run"`
	RunMetadata        interface{} `json:"run_metadata,omitempty"`
	RequestURL         string      `json:"request_url"`
	ResponseBody       []byte      `json:"response_body"` // Will be base64 encoded when JSON marshalled.
	ResponseStatusCode int         `json:"response_status_code"`
}

type S3ResultReporterConfig struct {
	Bucket string
	Prefix string
}

func newS3ResultReporterConfigFromDSN(dsn string) (S3ResultReporterConfig, error) {
	s3config := S3ResultReporterConfig{}

	u, err := url.Parse(dsn)
	if err != nil {
		return s3config, fmt.Errorf("invalid s3 result reporter DSN: %w", err)
	}

	// Extract bucket and optional prefix from the path
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
	if len(parts) == 0 {
		return s3config, fmt.Errorf("bucket name is required in S3 DSN")
	}

	s3config.Bucket = parts[0]
	if len(parts) > 1 {
		s3config.Prefix = parts[1]
	}

	return s3config, nil
}

// ReportResultToS3 stores results in S3 as JSON files. Results are grouped by run
// in a run specific directory. The directory structure is as follows:
//
//	<prefix>/<run_uuid>/<url_hash>.json
//
// The <url_hash> is the SHA-256 hash of the request URL, encoded as a hex string.
// The JSON file contains the result as a JSON object.
func ReportResultToS3(ctx context.Context, s3config S3ResultReporterConfig, run *Run, res *collector.Response) error {
	logger := slog.With("run", run.ID, "url", res.Request.URL)
	logger.Debug("Result reporter: Saving result to S3...")

	// Load AWS configuration using aws-sdk-go-v2/config package
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	// Create S3 client
	client := s3.NewFromConfig(awsCfg)

	result := &s3Result{
		Run:                run.ID,
		RequestURL:         res.Request.URL.String(),
		ResponseBody:       res.Body[:],
		ResponseStatusCode: res.StatusCode,
	}

	// Generate the object key
	hash := sha256.New()
	hash.Write([]byte(res.Request.URL.String()))
	filename := fmt.Sprintf("%s.json", hex.EncodeToString(hash.Sum(nil)))

	// Construct the full S3 key
	key := filepath.Join(s3config.Prefix, run.ID, filename)
	key = strings.TrimLeft(key, "/") // Remove leading slash as S3 doesn't need it

	// Marshal the result to JSON
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}

	// Upload to S3
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s3config.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(jsonData),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	logger.Debug("Result reporter: Successfully saved result to S3",
		"bucket", s3config.Bucket,
		"key", key,
	)
	return nil
}
