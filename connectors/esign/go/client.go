// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package esign reads eSign signing-flow status without returning signer details.
package esign

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strings"
	"time"
)

const signFlowEndpoint = "https://openapi.esign.cn"

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client resolves app signature credentials for each status request.
type Client struct {
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	clock            func() time.Time
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("eSign credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, errors.New("eSign configuration is invalid")
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("eSign timeout or response limit is invalid")
	}
	client := &Client{maxResponseBytes: config.MaxResponseBytes, credentials: credentials, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("eSign client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetSignFlowStatus returns the read-only signing status Query bound to this client.
func (client *Client) GetSignFlowStatus() GetSignFlowStatusOperation {
	return GetSignFlowStatusOperation{client: client}
}
func isValidCredential(value string) bool {
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
func isValidSignFlowID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}
