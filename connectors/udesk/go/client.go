// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package udesk reads ticket statuses through the Udesk Open API v2.
package udesk

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"strings"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for a proxy or provider test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable Udesk configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates the account subdomain without contacting Udesk. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Udesk credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Udesk timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, errors.New("Udesk base URL must be a valid HTTPS account origin")
	}
	if !isValidAccountOrigin(baseURL) {
		return nil, errors.New("Udesk base URL must be an HTTPS account subdomain of udesk.cn")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Udesk client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListTicketStatuses returns the status-list Query bound to this client.
func (client *Client) ListTicketStatuses() ListTicketStatusesOperation {
	return ListTicketStatusesOperation{client: client}
}
func isValidAccountOrigin(baseURL string) bool {
	endpoint, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(endpoint.Hostname())
	if endpoint.Path != "" && endpoint.Path != "/" || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.User != nil {
		return false
	}
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return endpoint.Scheme == "http" || endpoint.Scheme == "https"
	}
	if endpoint.Scheme != "https" || endpoint.Port() != "" || !strings.HasSuffix(host, ".udesk.cn") {
		return false
	}
	subdomain := strings.TrimSuffix(host, ".udesk.cn")
	if subdomain == "" || len(subdomain) > 63 || subdomain[0] == '-' || subdomain[len(subdomain)-1] == '-' {
		return false
	}
	for _, character := range subdomain {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}
func isValidToken(token string) bool {
	if token == "" || strings.TrimSpace(token) != token || len(token) > 512 {
		return false
	}
	for _, character := range token {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func isValidAdminEmail(email string) bool {
	if email == "" || len(email) > 320 || strings.TrimSpace(email) != email || strings.Count(email, "@") != 1 {
		return false
	}
	parts := strings.SplitN(email, "@", 2)
	return parts[0] != "" && strings.Contains(parts[1], ".") && !strings.ContainsAny(email, " \t\r\n&=?#")
}
