// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package cloopen reads Cloopen balance payment status without exposing payment credentials.
package cloopen

import (
	"errors"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"time"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }
type clockOption struct{ now func() time.Time }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

func (option clockOption) applyClientOption(client *Client) {
	client.now = option.now
}

// WithHTTPClient supplies an HTTP client for provider tests or approved proxies. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable Cloopen configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	now              func() time.Time
}

// New validates local settings without contacting Cloopen. Rotated credentials take effect on the next request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Cloopen credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Cloopen timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Cloopen base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Cloopen client option is required")
		}
		option.applyClientOption(client)
	}
	if client.now == nil {
		return nil, errors.New("Cloopen clock is required")
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetBalance returns the read-only balance Query bound to this client.
func (client *Client) GetBalance() GetBalanceOperation {
	return GetBalanceOperation{client: client}
}
