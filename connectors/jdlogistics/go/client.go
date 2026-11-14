// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package jdlogistics queries authorized B2C waybill scans through the JD Logistics LOP gateway.
package jdlogistics

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const queryPath = "/ecap/v1/orders/trace/query"

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
	clock            func() time.Time
}

// New validates JD Logistics configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("JD Logistics credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("JD Logistics gateway URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("JD Logistics gateway URL must be an HTTPS origin without path or query")
	}
	client := &Client{baseURL: strings.TrimSuffix(baseURL, "/"), maxResponseBytes: config.MaxResponseBytes, credentials: credentials, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("JD Logistics client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("JD Logistics credentials unavailable")
var errInput = errors.New("JD Logistics waybill input invalid")
var errRequest = errors.New("JD Logistics request failed")

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
	appKey, appSecret := credentials.AppKey, credentials.AppSecret.Reveal()
	accessToken, customerCode := credentials.AccessToken.Reveal(), credentials.CustomerCode
	if !isOpaqueCredential(appKey) || !isOpaqueCredential(appSecret) || !isOpaqueCredential(accessToken) || !isOpaqueCredential(customerCode) || len(customerCode) > 32 {
		return providerResponse{err: errCredential}
	}
	body, err := json.Marshal([]struct {
		WaybillCode  string `json:"waybillCode"`
		OrderOrigin  int    `json:"orderOrigin"`
		CustomerCode string `json:"customerCode"`
	}{{WaybillCode: input.WaybillNo, OrderOrigin: 1, CustomerCode: customerCode}})
	if err != nil {
		return providerResponse{err: errInput}
	}
	timestamp := client.clock().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	signed := appSecret + "access_token" + accessToken + "app_key" + appKey + "method" + queryPath + "param_json" + string(body) + "timestamp" + timestamp + "v2.0" + appSecret
	digest := md5.Sum([]byte(signed))
	// JD LOP gateway takes signed system parameters, including access_token, in the query: https://cloud.jdl.com/
	parameters := url.Values{"app_key": {appKey}, "access_token": {accessToken}, "timestamp": {timestamp}, "v": {"2.0"}, "LOP-DN": {"ECAP"}, "sign": {strings.ToUpper(hex.EncodeToString(digest[:]))}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+queryPath+"?"+parameters.Encode(), strings.NewReader(string(body)))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
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
	if value == "" || len(value) > 100 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
