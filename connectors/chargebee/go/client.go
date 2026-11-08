// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package chargebee reads selected Chargebee subscription status in Dex Flows.
package chargebee

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures an HTTP dependency without storing it in a connection.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable provider settings and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration and required dependencies without contacting Chargebee.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Chargebee credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Chargebee API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Chargebee client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Chargebee credential unavailable")
var errRequest = errors.New("Chargebee request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) get(call sdkgo.Call, path string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	apiKey := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiKey) || strings.Contains(apiKey, ":") {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(apiKey+":")))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
