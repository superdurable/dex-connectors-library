// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package microsoftadvertising exposes typed campaign Queries to Dex Flows.
package microsoftadvertising

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures an HTTP dependency without serializing it into a connection.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable account settings and resolves OAuth credentials per call.
type Client struct {
	baseURL          string
	customerID       string
	maxResponseBytes int64
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	httpClient       *http.Client
}

// New validates connection configuration without contacting Microsoft Advertising.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Microsoft Advertising credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if _, err := strconv.ParseInt(config.CustomerID, 10, 64); err != nil || !advertisingIDPattern.MatchString(config.CustomerID) {
		return nil, errors.New("Microsoft Advertising customer ID must be a positive decimal identifier")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Microsoft Advertising API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, customerID: config.CustomerID, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Microsoft Advertising client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.refreshDriver = NewCredentialRefreshDriver(client.httpClient)
	return client, nil
}
