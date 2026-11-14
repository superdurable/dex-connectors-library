// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package seniverse reads current weather through Seniverse Weather API.
package seniverse

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for a proxy or provider test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable Seniverse configuration and resolves credentials for each call. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	language         string
	unit             string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Seniverse. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Seniverse credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Seniverse timeout and maxResponseBytes must be positive")
	}
	if !isSupportedLanguage(config.Language) || (config.Unit != "c" && config.Unit != "f") {
		return nil, errors.New("Seniverse language or temperature unit is invalid")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Seniverse base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, language: config.Language, unit: config.Unit, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Seniverse client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetCurrentWeather returns the one-location current-weather Query bound to this client.
func (client *Client) GetCurrentWeather() GetCurrentWeatherOperation {
	return GetCurrentWeatherOperation{client: client}
}

func isSupportedLanguage(language string) bool {
	switch language {
	case "zh-Hans", "zh-Hant", "en", "ja", "de", "fr", "es", "pt", "hi", "id", "ru", "th", "ar":
		return true
	}
	return false
}
