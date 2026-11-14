// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package siliconflow reads the SiliconFlow China model catalog.
package siliconflow

import (
	"errors"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strings"
)

// Option supplies a non-serializable HTTP dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for provider tests. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable SiliconFlow configuration and resolves credentials for each call.
// It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting SiliconFlow. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("SiliconFlow credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("SiliconFlow timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("SiliconFlow base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("SiliconFlow client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListModels returns the model-catalog Query bound to this client.
func (client *Client) ListModels() ListModelsOperation { return ListModelsOperation{client: client} }
func isValidAPIKey(apiKey string) bool {
	if apiKey == "" || strings.TrimSpace(apiKey) != apiKey || len(apiKey) > 512 {
		return false
	}
	for _, character := range apiKey {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
