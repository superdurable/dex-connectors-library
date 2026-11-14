// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fanruan lists applications authorized by a Jiandaoyun API Key.
package fanruan

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const appsEndpoint = "https://api.jiandaoyun.com/api/v5/app/list"

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client resolves API-key credentials for each application-list request.
type Client struct {
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Jiandaoyun credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, errors.New("Jiandaoyun configuration is invalid")
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("Jiandaoyun timeout or response limit is invalid")
	}
	client := &Client{maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Jiandaoyun client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListAppsPage returns the read-only authorized application Query bound to this client.
func (client *Client) ListAppsPage() ListAppsPageOperation {
	return ListAppsPageOperation{client: client}
}

func isValidAPIKey(value string) bool {
	if value == "" || len(value) > 1024 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
