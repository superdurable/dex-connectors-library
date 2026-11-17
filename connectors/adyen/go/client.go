// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package adyen reads selected Adyen payment link status in Dex Flows.
package adyen

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
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

// New validates configuration and required dependencies without contacting Adyen.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Adyen credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Adyen API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Adyen client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Adyen credential unavailable")
var errRequest = errors.New("Adyen request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) get(call sdkgo.Call, path string) providerResponse {
	return client.request(call, http.MethodGet, path, nil, "")
}

func (client *Client) patch(call sdkgo.Call, path string, body []byte) providerResponse {
	return client.request(call, http.MethodPatch, path, body, "")
}

func (client *Client) post(call sdkgo.Call, path string, body []byte) providerResponse {
	return client.request(call, http.MethodPost, path, body, string(call.IdempotencyKey))
}

func (client *Client) queryPost(call sdkgo.Call, path string, body []byte) providerResponse {
	return client.request(call, http.MethodPost, path, body, "")
}

func (client *Client) request(call sdkgo.Call, method string, path string, body []byte, idempotencyKey string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	apiKey := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiKey) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-API-Key", apiKey)
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
