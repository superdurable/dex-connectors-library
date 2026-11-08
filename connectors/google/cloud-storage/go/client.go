// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package cloudstorage reads bounded Google Cloud Storage objects through Dex Queries.
package cloudstorage

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

// Client holds immutable Cloud Storage settings and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	httpClient       *http.Client
}

// New validates connection configuration without contacting Google.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Google Cloud Storage credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Google Cloud Storage API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Google Cloud Storage client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.refreshDriver = NewCredentialRefreshDriver(client.httpClient)
	return client, nil
}

var errCredential = errors.New("Google Cloud Storage credential unavailable")
var errRequest = errors.New("Google Cloud Storage request failed")
var errRedirect = errors.New("Google Cloud Storage redirect was unsafe")

type providerResponse struct {
	body        []byte
	contentType string
	status      int
	retryAfter  time.Duration
	err         error
}

func (client *Client) request(call sdkgo.Call, path string, media bool) providerResponse {
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
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if media {
		request.Header.Set("Accept", "*/*")
	} else {
		request.Header.Set("Accept", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	if media && (response.StatusCode == http.StatusFound || response.StatusCode == http.StatusTemporaryRedirect) {
		location, parseErr := url.Parse(response.Header.Get("Location"))
		_ = response.Body.Close()
		if parseErr != nil || location == nil || location.Scheme != "https" || location.User != nil || location.Port() != "" || !strings.HasSuffix(location.Hostname(), ".googleusercontent.com") {
			return providerResponse{err: errRedirect}
		}
		request, err = http.NewRequestWithContext(call.Context, http.MethodGet, location.String(), nil)
		if err != nil {
			return providerResponse{err: errRedirect}
		}
		request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
		response, err = client.httpClient.Do(request)
		if err != nil {
			return providerResponse{err: errRequest}
		}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return providerResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return providerResponse{body: body, contentType: response.Header.Get("Content-Type"), status: response.StatusCode, retryAfter: retryAfter, err: err}
}
