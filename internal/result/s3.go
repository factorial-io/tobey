// Copyright 2024 Factorial GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result

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
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Result struct {
	Run                string      `json:"run"`
	RunMetadata        interface{} `json:"run_metadata,omitempty"`
	RequestURL         string      `json:"request_url"`
	ResponseBody       []byte      `json:"response_body"` // Will be base64 encoded when JSON marshalled.
	ResponseStatusCode int         `json:"response_status_code"`
}

type S3Config struct {
	Bucket       string
	Prefix       string
	Endpoint     string
	Region       string
	UsePathStyle bool
}

// LogFormat returns a string representation of the config suitable for logging
func (c S3Config) LogFormat() string {
	return fmt.Sprintf("s3(bucket=%s, prefix=%s, endpoint=%s, region=%s, usePathStyle=%v)",
		c.Bucket, c.Prefix, c.Endpoint, c.Region, c.UsePathStyle)
}

// NewS3ConfigFromDSN parses a DSN and returns a S3Config.
// The DSN is expected to be in the format:
//
//	s3://<bucket>/<prefix>?endpoint=<endpoint>&region=<region>&usePathStyle=<true|false>
//
// If the endpoint is not provided, the default endpoint for the region will be used.
// If the region is not provided, the default region for the bucket will be used.
func NewS3ConfigFromDSN(dsn string) (S3Config, error) {
	config := S3Config{}

	u, err := url.Parse(dsn)
	if err != nil {
		return config, fmt.Errorf("invalid s3 result reporter DSN: %w", err)
	}

	// Extract bucket and optional prefix from the path
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
	if len(parts) == 0 {
		return config, fmt.Errorf("bucket name is required in S3 DSN")
	}

	config.Bucket = parts[0]
	if len(parts) > 1 {
		config.Prefix = parts[1]
	}

	q := u.Query()
	config.Endpoint = q.Get("endpoint")
	config.Region = q.Get("region")
	config.UsePathStyle = q.Get("usePathStyle") == "true"

	return config, nil
}

// ReportToS3 stores results in S3 as JSON files. Results are grouped by run
// in a run specific directory. The directory structure is as follows:
//
//	<prefix>/<run_uuid>/<url_hash>.json
//
// The <url_hash> is the SHA-256 hash of the request URL, encoded as a hex string.
// The JSON file contains the result as a JSON object.
func ReportToS3(ctx context.Context, config S3Config, runID string, res *collector.Response) error {
	logger := slog.With("run", runID, "url", res.Request.URL)
	logger.Debug("Result reporter: Saving result to S3...")

	var options []func(*awsconfig.LoadOptions) error

	if config.Region != "" {
		options = append(options, awsconfig.WithRegion(config.Region))
	}
	if config.Endpoint != "" {
		customEndpoint := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			if service == s3.ServiceID {
				return aws.Endpoint{
					URL:               config.Endpoint,
					SigningRegion:     config.Region,
					HostnameImmutable: true,
				}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		})
		options = append(options, awsconfig.WithEndpointResolverWithOptions(customEndpoint))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return err
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = config.UsePathStyle
	})

	result := &s3Result{
		Run:                runID,
		RequestURL:         res.Request.URL.String(),
		ResponseBody:       res.Body[:],
		ResponseStatusCode: res.StatusCode,
	}

	hash := sha256.New()
	hash.Write([]byte(res.Request.URL.String()))
	filename := fmt.Sprintf("%s.json", hex.EncodeToString(hash.Sum(nil)))

	key := filepath.Join(config.Prefix, runID, filename)
	key = strings.TrimLeft(key, "/") // Remove leading slash as S3 doesn't need it

	jsonData, err := json.MarshalIndent(result, "", " ")
	if err != nil {
		return err
	}

	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(config.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(jsonData),
		ContentType: aws.String("application/json"),
		Metadata: map[string]string{
			"run": runID,
		},
	})
	return err
}
