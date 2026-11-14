// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package getui reads aggregate push reports from Getui REST API v2.
package getui

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const apiBase = "https://restapi.getui.com/v2/"

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

type cachedToken struct {
	value     string
	expiresAt time.Time
}

// Client resolves app credentials and caches short-lived tokens in worker memory.
type Client struct {
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	tokenMu          sync.Mutex
	tokens           map[string]cachedToken
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Getui credential source is required")
	}
	config = withConfigDefaults(config)
	if config.Validate() != nil || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("Getui configuration is invalid")
	}
	client := &Client{maxResponseBytes: config.MaxResponseBytes, credentials: credentials, tokens: make(map[string]cachedToken)}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Getui client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetDailyPushReport returns the read-only daily push report Query.
func (client *Client) GetDailyPushReport() GetDailyPushReportOperation {
	return GetDailyPushReportOperation{client: client}
}
func isValidAppID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
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
