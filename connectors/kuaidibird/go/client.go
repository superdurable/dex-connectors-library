// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package kuaidibird queries parcel tracking through KuaidiBird's real-time API.
package kuaidibird

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test or proxy HTTP client. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable provider configuration and resolves credentials for every call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates the connection without contacting KuaidiBird. Credential rotations apply on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("KuaidiBird credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("KuaidiBird timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("KuaidiBird base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("KuaidiBird client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// QueryTracking binds the parcel-tracking Query to this client.
func (client *Client) QueryTracking() QueryTrackingOperation {
	return QueryTrackingOperation{client: client}
}
