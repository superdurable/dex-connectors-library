// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package shoplazza reads Shoplazza store metadata through the Admin OpenAPI.
package shoplazza

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for provider tests or an approved proxy.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds validated store configuration and resolves credentials for each call.
// It is safe for concurrent Steps.
type Client struct {
	shopURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

var shopDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.myshoplaza\.com$`)

// New validates the system domain without contacting Shoplazza.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Shoplazza credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	domain := strings.ToLower(strings.TrimSpace(config.ShopDomain))
	if !shopDomainPattern.MatchString(domain) || config.ShopDomain != domain {
		return nil, errors.New("Shoplazza shopDomain must be a lowercase *.myshoplaza.com hostname")
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Shoplazza timeout and maxResponseBytes must be positive")
	}
	client := &Client{shopURL: "https://" + domain, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Shoplazza client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetShop returns the read-only shop metadata Query bound to this client.
func (client *Client) GetShop() GetShopOperation { return GetShopOperation{client: client} }

func validateAccessToken(token string) error {
	if token == "" || len(token) > 2048 || strings.TrimSpace(token) != token || !providerhttp.IsHeaderSafeCredential(token) {
		return fmt.Errorf("invalid Shoplazza access token")
	}
	return nil
}
