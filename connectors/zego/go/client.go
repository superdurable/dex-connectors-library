// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package zego reads ZIM room member counts without exposing member identities.
package zego

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const roomUserEndpoint = "https://zim-api.zego.im/"

// Option supplies a client dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a test transport or trusted proxy. Redirects remain disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client resolves project credentials for each room-count request.
type Client struct {
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates local settings without making a provider request.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("ZEGO credential source is required")
	}
	config = withConfigDefaults(config)
	if config.Validate() != nil || config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("ZEGO configuration is invalid")
	}
	client := &Client{maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("ZEGO client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetRoomUserCount returns the read-only room-count Query bound to this client.
func (client *Client) GetRoomUserCount() GetRoomUserCountOperation {
	return GetRoomUserCountOperation{client: client}
}
func isValidProjectCredentials(credentials Credentials) bool {
	appID, err := strconv.ParseUint(credentials.AppID, 10, 32)
	if err != nil || appID == 0 || strconv.FormatUint(appID, 10) != credentials.AppID {
		return false
	}
	secret := credentials.ServerSecret.Reveal()
	if secret == "" || len(secret) > 1024 || strings.TrimSpace(secret) != secret {
		return false
	}
	for _, character := range secret {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func isValidRoomID(roomID string) bool {
	if roomID == "" || len(roomID) > 32 {
		return false
	}
	for _, character := range roomID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		if !strings.ContainsRune("!#$%&()+-:;<=>?@[]^_{}|~.", character) {
			return false
		}
	}
	return true
}
