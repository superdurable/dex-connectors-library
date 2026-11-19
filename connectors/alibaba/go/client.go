// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package alibaba lists ECS regions through Alibaba Cloud OpenAPI.
package alibaba

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for provider tests or approved proxies. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable ECS configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	endpointHost     string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	nonceSource      io.Reader
	clock            func() time.Time
}

// New validates local settings without calling Alibaba Cloud. Rotated credentials take effect on the next request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Alibaba Cloud credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Alibaba Cloud timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Alibaba Cloud base URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Host == "" {
		return nil, errors.New("Alibaba Cloud base URL host is invalid")
	}
	client := &Client{baseURL: baseURL, endpointHost: parsedURL.Host, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, nonceSource: rand.Reader, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Alibaba Cloud client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// DescribeRegions returns the ECS region Query bound to this client.
func (client *Client) DescribeRegions() DescribeRegionsOperation {
	return DescribeRegionsOperation{client: client}
}
