// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package volcengine lists ECS regions through signed Volcengine OpenAPI requests.
package volcengine

import (
	"errors"
	"fmt"
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

// WithHTTPClient supplies an HTTP client for a proxy or provider test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

type clockOption struct{ now func() time.Time }

func (option clockOption) applyClientOption(client *Client) { client.now = option.now }

// Client owns immutable ECS configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	host             string
	region           string
	service          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	now              func() time.Time
}

// New validates configuration without contacting ECS. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Volcengine credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Volcengine timeout and maxResponseBytes must be positive")
	}
	if !isValidSigningScope(config.Region) || !isValidSigningScope(config.Service) {
		return nil, errors.New("Volcengine region or service is invalid")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Volcengine base URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, errors.New("Volcengine base URL is invalid")
	}
	client := &Client{baseURL: baseURL, host: parsed.Host, region: config.Region, service: config.Service, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Volcengine client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// DescribeRegions returns the one-page ECS region listing Query bound to this client.
func (client *Client) DescribeRegions() DescribeRegionsOperation {
	return DescribeRegionsOperation{client: client}
}
