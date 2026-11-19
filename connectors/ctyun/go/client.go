// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package ctyun reads Knowledge Base Q&A resource metadata through CTYun's signed HTTPS OpenAPI.
package ctyun

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for provider tests or a proxy. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns CTYun configuration and credentials. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	now              func() time.Time
	requestID        func() (string, error)
}

// New validates configuration without contacting CTYun.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("CTYun credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("CTYun base URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host != "kqa-global.ctapi.ctyun.cn" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("CTYun base URL must be the documented HTTPS production origin")
	}
	client := &Client{
		baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials,
		now: time.Now, requestID: newRequestID,
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("client option is required")
		}
		option.applyClientOption(client)
	}
	if client.now == nil || client.requestID == nil {
		return nil, errors.New("CTYun request clock and ID generator are required")
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListKnowledgeBases returns the resource-list Query bound to this client.
func (client *Client) ListKnowledgeBases() ListKnowledgeBasesOperation {
	return ListKnowledgeBasesOperation{client: client}
}
