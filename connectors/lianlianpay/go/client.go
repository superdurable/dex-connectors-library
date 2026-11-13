// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package lianlianpay queries authorized merchant payment statuses through the LianLian Pay Open Platform.
package lianlianpay

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

// Client owns immutable LianLian Pay configuration and resolves credentials for each query. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting LianLian. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("LianLian credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("LianLian timeout and response limit must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("LianLian base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("LianLian client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// QueryPaymentStatus returns the payment-status Query bound to this client.
func (client *Client) QueryPaymentStatus() QueryPaymentStatusOperation {
	return QueryPaymentStatusOperation{client: client}
}
