// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package baidumaps resolves Chinese addresses through Baidu Maps Web Service geocoding.
package baidumaps

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for a proxy or provider test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable Baidu Maps configuration and resolves credentials for each call.
// It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Baidu Maps. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Baidu Maps credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Baidu Maps timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Baidu Maps base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Baidu Maps client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GeocodeAddress returns the address lookup Query bound to this client.
func (client *Client) GeocodeAddress() GeocodeAddressOperation {
	return GeocodeAddressOperation{client: client}
}

func (client *Client) geocodeURL(accessKey string, input GeocodeAddressInput) string {
	query := url.Values{"ak": {accessKey}, "address": {input.Address}, "output": {"json"}}
	if input.City != "" {
		query.Set("city", input.City)
	}
	return client.baseURL + "/geocoding/v3/?" + query.Encode()
}

func isValidAccessKey(accessKey string) bool {
	if accessKey == "" || strings.TrimSpace(accessKey) != accessKey || len(accessKey) > 256 {
		return false
	}
	for _, character := range accessKey {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
