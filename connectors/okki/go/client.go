// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package okki reads OKKI CRM customer field definitions without customer records.
package okki

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const fieldsPath = "/v1/company/fields?type=company"

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
	credentials      CredentialSource
	httpClient       *http.Client
	refreshDriver    *CredentialRefreshDriver
}

// New validates OKKI configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("OKKI credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("OKKI API URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("OKKI API URL must be an HTTPS origin without path or query")
	}
	client := &Client{baseURL: strings.TrimSuffix(baseURL, "/"), maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("OKKI client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.refreshDriver = NewCredentialRefreshDriver(client.httpClient)
	return client, nil
}

var errCredential = errors.New("OKKI credentials unavailable")
var errRequest = errors.New("OKKI request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) listCustomerFields(call sdkgo.Call) providerResponse {
	initialCredentials, initialErr := client.credentials.Resolve(call)
	if initialErr != nil || initialCredentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return providerResponse{err: errCredential}
		}
		return providerResponse{err: errRequest}
	}
	if credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	if !isOpaqueCredential(credentials.AccessToken.Reveal()) {
		return providerResponse{err: errCredential}
	}
	for attempt := 0; attempt < 2; attempt++ {
		response := client.sendCustomerFieldsRequest(call, credentials.AccessToken.Reveal())
		if response.status != http.StatusUnauthorized || attempt > 0 {
			return response
		}
		if _, supported := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supported {
			return response
		}
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if refreshErr != nil {
			if sdkgo.IsReauthorizationRequired(refreshErr) {
				return response
			}
			return providerResponse{err: errRequest}
		}
		if !isOpaqueCredential(replacement.AccessToken.Reveal()) {
			return response
		}
		credentials = replacement
	}
	return providerResponse{err: errRequest}
}

func (client *Client) sendCustomerFieldsRequest(call sdkgo.Call, token string) providerResponse {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+fieldsPath, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Authorization", token)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}
func isOpaqueCredential(value string) bool {
	if value == "" || len(value) > 8192 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
