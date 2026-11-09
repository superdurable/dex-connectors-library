// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package metaadsinsights reads Meta ad metadata through Dex Queries.
package metaadsinsights

import (
	"bytes"
	"encoding/json"
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

// Client holds immutable Graph API settings and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates connection configuration without contacting Meta.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Meta Ads Insights credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Meta Graph API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Meta Ads Insights client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Meta Ads Insights credential unavailable")
var errRequest = errors.New("Meta Ads Insights request failed")

type graphResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) get(call sdkgo.Call, path string) graphResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return graphResponse{err: errCredential}
	}
	accessToken := credentials.UserAccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return graphResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return graphResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return graphResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return graphResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return graphResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

// graphError is the subset of Meta's Graph API error envelope used for retry decisions.
type graphError struct {
	code        int
	isTransient bool
}

// decodeGraphError reports false when the body is not a Graph API error envelope.
func decodeGraphError(body []byte) (graphError, bool) {
	var envelope struct {
		Error *struct {
			Code        int  `json:"code"`
			IsTransient bool `json:"is_transient"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error == nil {
		return graphError{}, false
	}
	return graphError{code: envelope.Error.Code, isTransient: envelope.Error.IsTransient}, true
}

// isThrottled reports Meta's documented rate-limit codes, which Graph returns with HTTP 4xx.
func (graphErr graphError) isThrottled() bool {
	switch graphErr.code {
	case 4, 17, 32, 341, 613:
		return true
	}
	return graphErr.code >= 80000 && graphErr.code <= 80014
}
