// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package marketo reads selected Adobe Marketo Engage lead fields in Dex Flows.
package marketo

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures an HTTP dependency at construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Marketo settings and resolves renewable credentials per call.
type Client struct {
	restEndpoint     string
	tokenEndpoint    string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	refreshDriver    *CredentialRefreshDriver
}

// New validates both subscription endpoints and the credential source without contacting Marketo.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Marketo credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	restURL, err := validateSubscriptionURL(config.RESTEndpoint, "/rest")
	if err != nil {
		return nil, err
	}
	tokenURL, err := validateSubscriptionURL(config.TokenEndpoint, "/identity/oauth/token")
	if err != nil {
		return nil, err
	}
	if restURL.Hostname() != tokenURL.Hostname() {
		return nil, errors.New("Marketo REST and Identity URLs must use the same subscription host")
	}
	client := &Client{restEndpoint: strings.TrimRight(restURL.String(), "/"), tokenEndpoint: tokenURL.String(), maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Marketo client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	refreshHTTPClient := *client.httpClient
	refreshHTTPClient.Timeout = 8 * time.Second
	client.refreshDriver = NewCredentialRefreshDriver(&refreshHTTPClient, client.tokenEndpoint)
	return client, nil
}

func validateSubscriptionURL(raw string, suffix string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".mktorest.com") || parsed.Port() != "" || parsed.Path != suffix {
		return nil, errors.New("Marketo endpoint must be an HTTPS subscription URL ending in " + suffix)
	}
	return parsed, nil
}
