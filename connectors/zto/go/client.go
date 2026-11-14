// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package zto queries authorized waybill tracking scans through ZTO Open Platform.
package zto

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

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
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates ZTO configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("ZTO credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("ZTO API URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("ZTO API URL must be an origin without path or query")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("ZTO client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("ZTO credentials unavailable")
var errInput = errors.New("ZTO waybill input invalid")
var errRequest = errors.New("ZTO request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) queryWaybillTrace(call sdkgo.Call, input QueryWaybillTraceInput) providerResponse {
	if !isWaybillCode(input.BillCode) {
		return providerResponse{err: errInput}
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	appKey, appSecret := credentials.AppKey, credentials.AppSecret.Reveal()
	if !isOpaqueCredential(appKey) || !isOpaqueCredential(appSecret) {
		return providerResponse{err: errCredential}
	}
	body, err := json.Marshal(struct {
		BillCode string `json:"billCode"`
	}{input.BillCode})
	if err != nil {
		return providerResponse{err: errInput}
	}
	sum := md5.Sum(append(body, appSecret...))
	digest := base64.StdEncoding.EncodeToString(sum[:])
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/zto.merchant.waybill.track.query", strings.NewReader(string(body)))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-appKey", appKey)
	request.Header.Set("x-dataDigest", digest)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}
func isOpaqueCredential(value string) bool {
	if value == "" || len(value) > 4096 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func isWaybillCode(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
