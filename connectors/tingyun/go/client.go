// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tingyun lists monitored mobile applications without exposing app tokens.
package tingyun

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

// Client resolves Access Token credentials for each application-list request.
type Client struct {
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	endpoint         string
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Tingyun credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, errors.New("Tingyun configuration is invalid")
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("Tingyun timeout or response limit is invalid")
	}
	origin, err := validatedPlatformOrigin(config.APIHost)
	if err != nil {
		return nil, err
	}
	client := &Client{maxResponseBytes: config.MaxResponseBytes, credentials: credentials, endpoint: origin + "/app-api/app/list"}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Tingyun client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListApplications returns the read-only mobile application Query bound to this client.
func (client *Client) ListApplications() ListApplicationsOperation {
	return ListApplicationsOperation{client: client}
}

func validatedPlatformOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || parsed.Host == "" {
		return "", errors.New("Tingyun platform origin must be an HTTPS Tingyun host without path or port")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "tingyun.com" || !strings.HasSuffix(host, ".tingyun.com") || strings.Contains(host, "..") {
		return "", errors.New("Tingyun platform origin must be a Tingyun host")
	}
	return "https://" + host, nil
}
func isValidAccessToken(value string) bool {
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
