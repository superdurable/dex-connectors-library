// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package kuaishou reads the authorized user's videos through the official Open Platform.
package kuaishou

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures the HTTP transport without changing a connection.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable provider settings and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	pageSize         int
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates Kuaishou configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Kuaishou credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.PageSize < 1 || config.PageSize > 200 {
		return nil, errors.New("Kuaishou page size must be between 1 and 200")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Kuaishou API URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, errors.New("Kuaishou API URL must be an origin without a path or query")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, pageSize: int(config.PageSize), credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Kuaishou client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Kuaishou credentials unavailable")
var errInput = errors.New("Kuaishou cursor invalid")
var errRequest = errors.New("Kuaishou request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) listUserVideos(call sdkgo.Call, input ListUserVideosInput) providerResponse {
	if input.Cursor != "" && !isOpaqueValue(input.Cursor, 128) {
		return providerResponse{err: errInput}
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	appID, accessToken := credentials.AppID, credentials.AccessToken.Reveal()
	if !isOpaqueValue(appID, 4096) || !isOpaqueValue(accessToken, 4096) {
		return providerResponse{err: errCredential}
	}
	// Kuaishou Open Platform requires access_token as a query parameter: https://open.kuaishou.com/platform/openApi
	query := url.Values{"app_id": {appID}, "access_token": {accessToken}, "count": {strconv.Itoa(client.pageSize)}}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/openapi/photo/list?"+query.Encode(), nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}
func isOpaqueValue(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
