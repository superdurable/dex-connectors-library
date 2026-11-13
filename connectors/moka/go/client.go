// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package moka lists recruitment job functions without candidate or recruiter details.
package moka

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const jobFunctionsEndpointPrefix = "https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/"

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client resolves Moka credentials for each job-function request.
type Client struct {
	organizationID   string
	mode             string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Moka credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, errors.New("Moka configuration is invalid")
	}
	if !isValidOrganizationID(config.OrgID) || (config.Mode != "social" && config.Mode != "campus") || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("Moka organization, mode, timeout, or response limit is invalid")
	}
	client := &Client{organizationID: config.OrgID, mode: config.Mode, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Moka client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListJobFunctions returns the read-only job-function Query bound to this client.
func (client *Client) ListJobFunctions() ListJobFunctionsOperation {
	return ListJobFunctionsOperation{client: client}
}

func isValidOrganizationID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
func isValidAPIKey(value string) bool {
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
