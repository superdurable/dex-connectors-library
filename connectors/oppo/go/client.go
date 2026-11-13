// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package oppo reads OPPO Push daily sending quotas.
package oppo

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const authEndpoint = "https://api.push.oppomobile.com/server/v1/auth"
const pushQuotaEndpoint = "https://feedback.push.oppomobile.com/server/v1/feedback/fetch_push_permit"

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

// Client resolves OPPO Push credentials for each quota request.
type Client struct {
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	clock            func() time.Time
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("oppo credential source is required")
	}
	config = withConfigDefaults(config)
	if config.Validate() != nil || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("oppo configuration is invalid")
	}
	client := &Client{maxResponseBytes: config.MaxResponseBytes, credentials: credentials, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("oppo client option is required")
		}
		option.applyClientOption(client)
	}
	if client.clock == nil {
		return nil, errors.New("oppo clock is required")
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetPushQuota returns the daily Push quota Query bound to this client.
func (client *Client) GetPushQuota() GetPushQuotaOperation {
	return GetPushQuotaOperation{client: client}
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
