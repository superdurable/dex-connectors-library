// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package greenhouse reads selected candidate fields from the Harvest v3 API in Dex Flows.
package greenhouse

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

// Option configures an HTTP dependency without storing it in a connection.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Harvest settings and resolves access tokens per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	refreshDriver    *CredentialRefreshDriver
}

// New validates configuration and required dependencies without contacting Greenhouse.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Greenhouse credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Greenhouse Harvest URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || !strings.HasSuffix(parsed.Path, "/v3") {
		return nil, errors.New("Greenhouse Harvest URL must end in /v3")
	}
	client := &Client{baseURL: strings.TrimRight(baseURL, "/"), maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Greenhouse client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.refreshDriver = NewCredentialRefreshDriver(client.httpClient)
	return client, nil
}

var errCredential = errors.New("Greenhouse credential unavailable")
var errRequest = errors.New("Greenhouse request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) get(call sdkgo.Call, candidateID int64) providerResponse {
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
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return providerResponse{err: errCredential}
	}
	for attempt := 0; attempt < 2; attempt++ {
		response := client.sendCandidateRequest(call, credentials.AccessToken.Reveal(), candidateID)
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
		if !providerhttp.IsHeaderSafeCredential(replacement.AccessToken.Reveal()) {
			return response
		}
		credentials = replacement
	}
	return providerResponse{err: errRequest}
}

func (client *Client) sendCandidateRequest(call sdkgo.Call, token string, candidateID int64) providerResponse {
	query := url.Values{"ids": {fmt.Sprint(candidateID)}, "per_page": {"1"}, "fields": {"id,first_name,last_name"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/candidates?"+query.Encode(), nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
