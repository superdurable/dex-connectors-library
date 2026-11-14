// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package rongcloud checks IM user presence through RongCloud's domestic Server API.
package rongcloud

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for a proxy or provider test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable RongCloud configuration and resolves credentials for each call.
// It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting RongCloud. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("RongCloud credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("RongCloud timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("RongCloud base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("RongCloud client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// CheckUserOnline returns the online-status Query bound to this client.
func (client *Client) CheckUserOnline() CheckUserOnlineOperation {
	return CheckUserOnlineOperation{client: client}
}
func isValidCredentialValue(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 512 {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func createNonce() (string, error) {
	nonce := make([]byte, 9)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce), nil
}
func signRequest(appSecret, nonce, timestamp string) string {
	sum := sha1.Sum([]byte(appSecret + nonce + timestamp))
	return hex.EncodeToString(sum[:])
}
