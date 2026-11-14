// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package hesi reads Hesi department metadata through its HTTPS OpenAPI.
package hesi

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

// Client owns immutable Hesi configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Hesi. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Hesi credential source is required")
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
		return nil, fmt.Errorf("Hesi base URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || !isAllowedHesiHost(parsed.Host) {
		return nil, errors.New("Hesi base URL must be one of the documented HTTPS platform origins")
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

// ListDepartments returns the department-list Query bound to this client.
func (client *Client) ListDepartments() ListDepartmentsOperation {
	return ListDepartmentsOperation{client: client}
}

func isAllowedHesiHost(host string) bool {
	switch host {
	case "app.ekuaibao.com", "dd2.hosecloud.com", "wx2.ekuaibao.com", "kdcloud2.ekuaibao.com", "ddenterprise.ekuaibao.com", "feishu.ekuaibao.com":
		return true
	}
	return false
}
