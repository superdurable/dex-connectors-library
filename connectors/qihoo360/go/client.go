// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package qihoo360 implements bounded 360GPT text generation as a Dex Connector Query.
package qihoo360

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
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

// Client holds immutable model settings and resolves credentials on each generation attempt.
type Client struct {
	model            string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates local settings and constructs a client without contacting Qihoo360.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Qihoo360 credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !isValidModelID(config.Model) || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("Qihoo360 model, timeout, or response limit is invalid")
	}
	client := &Client{model: config.Model, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Qihoo360 client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GenerateText returns the text generation Query bound to this client.
func (client *Client) GenerateText() GenerateTextOperation {
	return GenerateTextOperation{client: client}
}
func isValidModelID(model string) bool {
	if len(model) < 1 || len(model) > 128 || strings.TrimSpace(model) != model {
		return false
	}
	for _, character := range model {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' || character == '/') {
			return false
		}
	}
	return true
}
func isValidAPIKey(key string) bool {
	if key == "" || len(key) > 512 || strings.TrimSpace(key) != key {
		return false
	}
	for _, character := range key {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
