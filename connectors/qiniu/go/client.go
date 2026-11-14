// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package qiniu lists Qiniu Kodo storage buckets through the management API.
package qiniu

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for a proxy or provider test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

type clockOption struct{ now func() time.Time }

func (option clockOption) applyClientOption(client *Client) { client.now = option.now }

// WithClock replaces the clock that stamps the signed X-Qiniu-Date header. The
// default is time.Now; Qiniu rejects a request more than 15 minutes from its clock.
func WithClock(now func() time.Time) Option { return clockOption{now} }

// Client owns immutable Qiniu configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	now              func() time.Time
}

// New validates configuration without contacting Qiniu. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Qiniu credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Qiniu timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Qiniu base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Qiniu client option is required")
		}
		option.applyClientOption(client)
	}
	if client.now == nil {
		return nil, errors.New("Qiniu clock is required")
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListBuckets returns the bucket-list Query bound to this client.
func (client *Client) ListBuckets() ListBucketsOperation { return ListBucketsOperation{client: client} }

func isValidCredential(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f || character == ':' {
			return false
		}
	}
	return true
}
