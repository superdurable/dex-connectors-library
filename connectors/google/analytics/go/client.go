// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package analytics4 reads Google Analytics 4 reports, properties, and audiences through Dex Queries.
package analytics4

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies an HTTP dependency without storing it in a connection.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Google Analytics 4 API settings and resolves credentials per call.
type Client struct {
	dataBaseURL      string
	adminBaseURL     string
	maxResponseBytes int64
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	httpClient       *http.Client
}

// New validates connection configuration without contacting Google Analytics 4.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Google Analytics 4 credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	dataBaseURL, err := providerhttp.ValidateBaseURL(config.DataBaseURL)
	if err != nil {
		return nil, fmt.Errorf("Google Analytics 4 API URL: %w", err)
	}
	adminBaseURL, err := providerhttp.ValidateBaseURL(config.AdminBaseURL)
	if err != nil {
		return nil, fmt.Errorf("Google Analytics 4 Admin API URL: %w", err)
	}
	client := &Client{dataBaseURL: dataBaseURL, adminBaseURL: adminBaseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Google Analytics 4 client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.refreshDriver = NewCredentialRefreshDriver(client.httpClient)
	return client, nil
}

var errCredential = errors.New("Google Analytics 4 credential unavailable")
var errRequest = errors.New("Google Analytics 4 request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) request(call sdkgo.Call, baseURL, method, path string, body []byte) providerResponse {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if sdkgo.IsReauthorizationRequired(err) {
		return providerResponse{err: errCredential}
	}
	if err != nil {
		return providerResponse{err: errRequest}
	}
	if credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, method, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err = providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return providerResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
