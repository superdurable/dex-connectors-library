// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fxiaoke lists CRM object metadata using an enterprise application's credentials.
package fxiaoke

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds OpenAPI settings and a short-lived, in-memory access-token cache.
type Client struct {
	baseURL           string
	employeeID        string
	maxResponseBytes  int64
	credentials       CredentialSource
	httpClient        *http.Client
	tokenMu           sync.Mutex
	tokenKey          [32]byte
	token             string
	enterpriseAccount string
	tokenValidUntil   time.Time
}

// New validates the cloud host and employee context without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("FXiaoke credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, errors.New("FXiaoke configuration is invalid")
	}
	if !isValidCloudHost(config.CloudHost) || !isValidIdentifier(config.EmployeeID) || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("FXiaoke cloud host, employee ID, timeout, or response limit is invalid")
	}
	client := &Client{baseURL: "https://" + config.CloudHost, employeeID: config.EmployeeID, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("FXiaoke client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListObjectMetadata returns the read-only CRM object-list Query bound to this client.
func (client *Client) ListObjectMetadata() ListObjectMetadataOperation {
	return ListObjectMetadataOperation{client: client}
}

func isValidCloudHost(host string) bool {
	if host == "open.fxiaoke.com" {
		return true
	}
	if !strings.HasPrefix(host, "open-") || len(host) > 128 || strings.Contains(host, "..") || strings.Contains(host, "-.") || strings.Contains(host, ".-") {
		return false
	}
	if !strings.HasSuffix(host, ".fxiaoke.com") && !strings.HasSuffix(host, ".sharecrm.com") {
		return false
	}
	for _, character := range host {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '.') {
			return false
		}
	}
	return true
}
func isValidIdentifier(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
func isValidSecret(value string) bool {
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
func credentialKey(credentials Credentials) [32]byte {
	return sha256.Sum256([]byte(credentials.AppID + "\x00" + credentials.AppSecret.Reveal() + "\x00" + credentials.PermanentCode.Reveal()))
}
