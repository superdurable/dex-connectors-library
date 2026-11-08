// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package todo reads Microsoft To Do lists and tasks through Microsoft Graph in Dex Flows.
package todo

import (
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures a client dependency at construction.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP transport, including a provider test transport.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Graph settings and resolves OAuth credentials per call.
type Client struct {
	credentials      CredentialSource
	httpClient       *http.Client
	refreshDriver    *CredentialRefreshDriver
	maxResponseBytes int64
}

// New validates the configuration and credential source without contacting Microsoft.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Microsoft To Do credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	client := &Client{credentials: credentials, maxResponseBytes: config.MaxResponseBytes}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Microsoft To Do client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, 15*time.Second)
	refreshHTTPClient := *client.httpClient
	refreshHTTPClient.Timeout = 8 * time.Second
	client.refreshDriver = NewCredentialRefreshDriver(&refreshHTTPClient)
	return client, nil
}

var errCredential = errors.New("Microsoft To Do credential unavailable")
