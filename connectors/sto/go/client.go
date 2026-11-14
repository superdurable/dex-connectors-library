// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sto queries authorized waybill tracking scans through STO Link gateway.
package sto

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

// New validates STO configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("STO credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("STO gateway URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Path != "/gateway/link.do" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("STO gateway URL must end in /gateway/link.do without query")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("STO client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("STO credentials unavailable")
var errInput = errors.New("STO waybill input invalid")
var errRequest = errors.New("STO request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) queryWaybillTrace(call sdkgo.Call, input QueryWaybillTraceInput) providerResponse {
	if !isWaybillNumber(input.WaybillNo) {
		return providerResponse{err: errInput}
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	appKey, appSecret, fromCode := credentials.AppKey, credentials.AppSecret.Reveal(), credentials.FromCode
	if !isOpaqueCredential(appKey) || !isOpaqueCredential(appSecret) || !isOpaqueCredential(fromCode) {
		return providerResponse{err: errCredential}
	}
	content, err := json.Marshal(struct {
		WaybillNoList []string `json:"waybillNoList"`
	}{[]string{input.WaybillNo}})
	if err != nil {
		return providerResponse{err: errInput}
	}
	sum := md5.Sum(append(content, appSecret...))
	fields := url.Values{"api_name": {"STO_TRACE_QUERY_COMMON"}, "content": {string(content)}, "from_appkey": {appKey}, "from_code": {fromCode}, "to_appkey": {"sto_trace_query"}, "to_code": {"sto_trace_query"}, "data_digest": {base64.StdEncoding.EncodeToString(sum[:])}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL, strings.NewReader(fields.Encode()))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
func isWaybillNumber(value string) bool {
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
