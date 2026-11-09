// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package creatio reads selected Creatio OData 4 contact fields in Dex Flows.
package creatio

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures the HTTP dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Creatio URLs and resolves renewable credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	refreshDriver    *CredentialRefreshDriver
}

// New validates connection settings without contacting Creatio.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Creatio credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Creatio application URL: %w", err)
	}
	tokenEndpoint, err := providerhttp.ValidateBaseURL(config.TokenEndpoint)
	if err != nil {
		return nil, fmt.Errorf("Creatio Identity Service token URL: %w", err)
	}
	tokenURL, err := url.Parse(tokenEndpoint)
	if err != nil || !strings.HasSuffix(tokenURL.Path, "/connect/token") || tokenURL.RawQuery != "" || tokenURL.Fragment != "" {
		return nil, errors.New("Creatio Identity Service token URL must end in /connect/token")
	}
	client := &Client{baseURL: strings.TrimRight(baseURL, "/"), maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Creatio client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	refreshClient := *client.httpClient
	refreshClient.Timeout = 8 * time.Second
	client.refreshDriver = NewCredentialRefreshDriver(&refreshClient, tokenEndpoint)
	return client, nil
}
