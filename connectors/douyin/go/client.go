// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package douyin reads aggregate mini app behavior metrics from Douyin Open Platform.
package douyin

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const tokenEndpoint = "https://open.douyin.com/oauth/client_token/"
const behaviorEndpoint = "https://open.douyin.com/api/platform/v2/data_analysis/query_behavior_data/"

type cachedToken struct {
	value     string
	expiresAt time.Time
}

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client resolves mini app credentials and caches short-lived tokens in worker memory.
type Client struct {
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	clock            func() time.Time
	tokenMu          sync.Mutex
	tokens           map[string]cachedToken
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Douyin credential source is required")
	}
	config = withConfigDefaults(config)
	if config.Validate() != nil || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("Douyin configuration is invalid")
	}
	client := &Client{maxResponseBytes: config.MaxResponseBytes, credentials: credentials, clock: time.Now, tokens: make(map[string]cachedToken)}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Douyin client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetBehaviorSummary returns the read-only mini app behavior Query.
func (client *Client) GetBehaviorSummary() GetBehaviorSummaryOperation {
	return GetBehaviorSummaryOperation{client: client}
}
func isValidOpaqueValue(value string) bool {
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
