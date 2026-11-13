// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sensorsdata reads campaign metadata through Sensors Data Analytics OpenAPI.
package sensorsdata

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

// Client owns immutable Analytics configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	projectName      string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Sensors Data. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Sensors Data credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("timeout and maxResponseBytes must be positive")
	}
	if !isValidProjectName(config.ProjectName) {
		return nil, errors.New("Sensors Data projectName must be a single ASCII identifier")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Sensors Data base URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("Sensors Data base URL must be an HTTPS origin without a path")
	}
	client := &Client{baseURL: baseURL, projectName: config.ProjectName, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListCampaigns returns the channel campaign Query bound to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation {
	return ListCampaignsOperation{client: client}
}
