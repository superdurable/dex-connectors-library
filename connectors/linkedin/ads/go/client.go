// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package ads exposes typed LinkedIn Advertising Queries to Dex Flows.
package ads

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures an HTTP dependency without serializing it into a connection.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable LinkedIn Ads settings and resolves OAuth credentials per call.
type Client struct {
	baseURL          string
	marketingVersion string
	maxResponseBytes int64
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	httpClient       *http.Client
}

// New validates connection configuration without contacting LinkedIn Ads.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("LinkedIn Ads credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !marketingVersionPattern.MatchString(config.MarketingVersion) {
		return nil, errors.New("LinkedIn Marketing API version must be YYYYMM")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("LinkedIn Ads API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, marketingVersion: config.MarketingVersion, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("LinkedIn Ads client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.refreshDriver = NewCredentialRefreshDriver(client.httpClient)
	return client, nil
}

var errCredential = errors.New("LinkedIn Ads credential unavailable")
var errRequest = errors.New("LinkedIn Ads request failed")
var marketingVersionPattern = regexp.MustCompile(`^[0-9]{4}(0[1-9]|1[0-2])$`)

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) get(call sdkgo.Call, path string) providerResponse {
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
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Linkedin-Version", client.marketingVersion)
	request.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return providerResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
