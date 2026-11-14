// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package oceanbase lists OceanBase Cloud instances using HTTP Digest authentication.
package oceanbase

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for provider tests. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable service-site configuration and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting OceanBase Cloud.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("OceanBase credential source is required")
	}
	config = withConfigDefaults(config)
	if config.Validate() != nil || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("OceanBase configuration is invalid")
	}
	baseURL := "https://api-cloud-cn.oceanbase.com"
	if config.Site == SiteInternational {
		baseURL = "https://api-cloud.oceanbase.com"
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("OceanBase client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListInstances returns the instance-list Query bound to this client.
func (client *Client) ListInstances() ListInstancesOperation {
	return ListInstancesOperation{client: client}
}
func isValidCredential(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 512 {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func isValidProjectID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
