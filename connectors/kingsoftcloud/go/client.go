// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package kingsoftcloud lists Kingsoft Cloud IAM users through signed OpenAPI requests.
package kingsoftcloud

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

type clockOption struct{ now func() time.Time }

func (option clockOption) applyClientOption(client *Client) { client.now = option.now }

// WithHTTPClient supplies an HTTP client for a proxy or provider test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable IAM configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	region           string
	service          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	now              func() time.Time
}

// New validates configuration without contacting IAM. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Kingsoft Cloud credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Kingsoft Cloud timeout and maxResponseBytes must be positive")
	}
	if config.Region == "" || config.Service == "" {
		return nil, errors.New("Kingsoft Cloud region and service are required")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Kingsoft Cloud base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, region: config.Region, service: config.Service, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Kingsoft Cloud client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListUsers returns the one-page IAM user listing Query bound to this client.
func (client *Client) ListUsers() ListUsersOperation { return ListUsersOperation{client: client} }
