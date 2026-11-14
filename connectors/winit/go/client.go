// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package winit lists authorized overseas warehouse metadata through the Winit OpenAPI.
package winit

import (
	"errors"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for provider tests or a trusted proxy. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable Winit configuration and resolves credentials for each query. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	platform         string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Winit. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Winit credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 || !validPlatform(config.Platform) {
		return nil, errors.New("Winit timeout, response limit, and platform must be valid")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Winit base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, platform: config.Platform, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Winit client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListWarehouses returns the authorized warehouse Query bound to this client.
func (client *Client) ListWarehouses() ListWarehousesOperation {
	return ListWarehousesOperation{client: client}
}
