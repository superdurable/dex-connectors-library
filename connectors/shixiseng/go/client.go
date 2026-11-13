// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package shixiseng reads public internship industry categories.
package shixiseng

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const industriesEndpoint = "https://open.shixiseng.com/industry/list"

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

type clockOption struct{ now func() time.Time }

func (option clockOption) applyClientOption(client *Client) { client.clock = option.now }

// WithClock supplies a trusted timestamp source for signed requests.
func WithClock(now func() time.Time) Option { return clockOption{now: now} }

// Client resolves merchant credentials for each industry-list request.
type Client struct {
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	clock            func() time.Time
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("shixiseng credential source is required")
	}
	config = withConfigDefaults(config)
	if config.Validate() != nil || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("shixiseng configuration is invalid")
	}
	client := &Client{maxResponseBytes: config.MaxResponseBytes, credentials: credentials, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("shixiseng client option is required")
		}
		option.applyClientOption(client)
	}
	if client.clock == nil {
		return nil, errors.New("shixiseng clock is required")
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListIndustries returns the public industry-list Query bound to this client.
func (client *Client) ListIndustries() ListIndustriesOperation {
	return ListIndustriesOperation{client: client}
}

func isValidCredential(value string) bool {
	if value == "" || len(value) > 1024 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
