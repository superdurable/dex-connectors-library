// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package aqara lists authorized devices through Aqara's mainland China Cloud API.
package aqara

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
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

// WithHTTPClient supplies an HTTP client for a provider test or trusted proxy. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable Aqara configuration and resolves credentials on each query. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	nonceSource      io.Reader
	clock            func() time.Time
}

// New validates configuration without contacting Aqara. Changed credentials take effect on the next query.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Aqara credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Aqara timeout and response limit must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Aqara base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, nonceSource: rand.Reader, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Aqara client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListDevices returns the one-page device Query bound to this client.
func (client *Client) ListDevices() ListDevicesOperation { return ListDevicesOperation{client: client} }
