// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package cainiao queries electronic-waybill events through Alibaba TOP.
package cainiao

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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
	clock            func() time.Time
}

// New validates Cainiao configuration without contacting TOP.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Cainiao credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Cainiao TOP URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" || (parsedURL.Path != "" && parsedURL.Path != "/router/rest") {
		return nil, errors.New("Cainiao TOP URL must be a router endpoint without query parameters")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Cainiao client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Cainiao credentials unavailable")
var errInput = errors.New("Cainiao waybill input invalid")
var errRequest = errors.New("Cainiao request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) queryWaybillTrace(call sdkgo.Call, input QueryWaybillTraceInput) providerResponse {
	if !isWaybillField(input.CarrierCode, 32) || !isWaybillField(input.WaybillCode, 128) {
		return providerResponse{err: errInput}
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	appKey, appSecret, sessionKey := credentials.AppKey, credentials.AppSecret.Reveal(), credentials.SessionKey.Reveal()
	if !isOpaqueCredential(appKey) || !isOpaqueCredential(appSecret) || !isOpaqueCredential(sessionKey) {
		return providerResponse{err: errCredential}
	}
	fields := url.Values{
		"app_key": {appKey}, "method": {"cainiao.waybill.logisticstrace.get"}, "format": {"json"},
		"session": {sessionKey}, "sign_method": {"hmac"}, "timestamp": {client.clock().In(time.FixedZone("GMT+8", 8*3600)).Format("2006-01-02 15:04:05")}, "v": {"2.0"},
		"cp_code": {input.CarrierCode}, "waybill_code": {input.WaybillCode},
	}
	fields.Set("sign", signRequest(appSecret, fields))
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL, strings.NewReader(fields.Encode()))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), client.clock()), err: err}
}

func signRequest(appSecret string, fields url.Values) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if key != "sign" && fields.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for _, key := range keys {
		canonical.WriteString(key)
		canonical.WriteString(fields.Get(key))
	}
	signer := hmac.New(md5.New, []byte(appSecret))
	_, _ = signer.Write([]byte(canonical.String()))
	return strings.ToUpper(hex.EncodeToString(signer.Sum(nil)))
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
func isWaybillField(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
