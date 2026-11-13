// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package enlvon searches Chinese electricity policy cards through Enlvon's HTTPS API.
package enlvon

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for provider tests or a proxy. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns Enlvon configuration and credentials. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Enlvon.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Enlvon credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Enlvon base URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host != "enlvon.com" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Enlvon base URL must be the documented HTTPS production origin")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// SearchPolicies returns the policy-search Query bound to this client.
func (client *Client) SearchPolicies() SearchPoliciesOperation {
	return SearchPoliciesOperation{client: client}
}
