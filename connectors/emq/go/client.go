// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package emq reads EMQX Cloud Platform API deployment metadata.
package emq

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"strings"
)

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable API settings and resolves Platform credentials for each call.
type Client struct {
	endpoint         string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates the Platform API endpoint without contacting EMQX Cloud.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("EMQX Cloud credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, errors.New("EMQX Cloud configuration is invalid")
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("EMQX Cloud timeout or response limit is invalid")
	}
	if !isValidPlatformEndpoint(config.PlatformEndpoint) {
		return nil, errors.New("EMQX Cloud Platform endpoint must be an HTTPS URL ending in /public_api/v1")
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.PlatformEndpoint)
	if err != nil {
		return nil, errors.New("EMQX Cloud Platform endpoint must be an HTTPS URL ending in /public_api/v1")
	}
	client := &Client{endpoint: endpoint, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("EMQX Cloud client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListDeployments returns the deployment-list Query bound to this client.
func (client *Client) ListDeployments() ListDeploymentsOperation {
	return ListDeploymentsOperation{client: client}
}
func isValidPlatformEndpoint(value string) bool {
	endpoint, err := url.Parse(value)
	if err != nil {
		return false
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.RawPath != "" || endpoint.Path != "/public_api/v1" || endpoint.Hostname() == "" || strings.HasSuffix(value, "/") {
		return false
	}
	if endpoint.Scheme == "https" {
		return true
	}
	host := endpoint.Hostname()
	return endpoint.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}
func isValidBasicCredential(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
