// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package baichuan reads Baichuan AI knowledge bases.
package baichuan

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

// Client owns immutable Baichuan AI configuration and resolves credentials for each call.
// It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Baichuan AI. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Baichuan AI credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Baichuan AI timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Baichuan AI base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Baichuan AI client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListKnowledgeBases returns the knowledge-base-list Query bound to this client.
func (client *Client) ListKnowledgeBases() ListKnowledgeBasesOperation {
	return ListKnowledgeBasesOperation{client: client}
}
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
