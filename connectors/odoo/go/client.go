// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package odoo searches company partners through Odoo's JSON-2 API.
package odoo

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport for provider tests or a private Odoo deployment.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Odoo settings and resolves API keys for each call.
type Client struct {
	baseURL          string
	database         string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates Odoo JSON-2 settings without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Odoo credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Odoo base URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("Odoo base URL must be an origin without a path")
	}
	if !providerhttp.IsHeaderSafeCredential(config.Database) || len(config.Database) > 128 {
		return nil, errors.New("Odoo database name is invalid")
	}
	client := &Client{baseURL: baseURL, database: config.Database, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Odoo client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Odoo API key unavailable")
var errRequest = errors.New("Odoo request failed")

type providerResponse struct {
	status     int
	body       []byte
	retryAfter time.Duration
	err        error
}

func (client *Client) search(call sdkgo.Call, body []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/json/2/res.partner/search_read", bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("X-Odoo-Database", client.database)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{status: response.StatusCode, body: responseBody, retryAfter: retryAfter, err: err}
}

func validNamePattern(pattern string) bool {
	return pattern != "" && len(pattern) <= 256 && utf8.ValidString(pattern) && strings.TrimSpace(pattern) == pattern && !strings.ContainsAny(pattern, "\x00\r\n")
}

func partnerFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "odoo", Operation: "searchPartners", Message: message}
}

func partnerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PartnerSearchPage] {
	failure := partnerFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PartnerSearchPage{}, &failure, sdkgo.Receipt{})
}

func rejectedStatus(status int) string { return "Odoo rejected HTTP " + strconv.Itoa(status) }
