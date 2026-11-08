// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package posthog captures events and lists saved insights through typed Dex Steps.
package posthog

import (
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable endpoint settings and resolves credentials per call.
type Client struct {
	captureURL       string
	appURL           string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates endpoints without contacting PostHog.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("PostHog credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	captureURL, err := providerhttp.ValidateBaseURL(config.CaptureURL)
	if err != nil {
		return nil, fmt.Errorf("PostHog capture URL: %w", err)
	}
	appURL, err := providerhttp.ValidateBaseURL(config.AppURL)
	if err != nil {
		return nil, fmt.Errorf("PostHog app URL: %w", err)
	}
	client := &Client{captureURL: captureURL, appURL: appURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("PostHog client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

func posthogRejectionKind(status int) sdkgo.FailureKind {
	if status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}
