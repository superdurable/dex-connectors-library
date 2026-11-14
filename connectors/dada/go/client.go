// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package dada queries delivery order status through the Dada Open Platform.
package dada

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
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

// New validates Dada configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Dada credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Dada API URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("Dada API URL must be a host root without a path")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Dada client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Dada credentials unavailable")
var errInput = errors.New("Dada order ID invalid")
var errRequest = errors.New("Dada request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) queryOrderStatus(call sdkgo.Call, input QueryOrderStatusInput) providerResponse {
	if !isOrderID(input.OrderID) {
		return providerResponse{err: errInput}
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	appKey := credentials.AppKey
	appSecret := credentials.AppSecret.Reveal()
	sourceID := credentials.SourceID
	if !isOpaqueCredential(appKey) || !isOpaqueCredential(appSecret) || !isSourceID(sourceID) {
		return providerResponse{err: errCredential}
	}
	businessBody, err := json.Marshal(struct {
		OrderID string `json:"order_id"`
	}{input.OrderID})
	if err != nil {
		return providerResponse{err: errInput}
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	bodyString := string(businessBody)
	signature := signRequest(appSecret, appKey, bodyString, sourceID, timestamp)
	envelope := struct {
		AppKey    string `json:"app_key"`
		Body      string `json:"body"`
		Format    string `json:"format"`
		Signature string `json:"signature"`
		SourceID  string `json:"source_id"`
		Timestamp string `json:"timestamp"`
		Version   string `json:"v"`
	}{AppKey: appKey, Body: bodyString, Format: "json", Signature: signature, SourceID: sourceID, Timestamp: timestamp, Version: "1.0"}
	requestBody, err := json.Marshal(envelope)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/api/order/status/query", bytes.NewReader(requestBody))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func signRequest(appSecret string, appKey string, body string, sourceID string, timestamp string) string {
	sortedFields := "app_key" + appKey + "body" + body + "formatjson" + "source_id" + sourceID + "timestamp" + timestamp + "v1.0"
	digest := md5.Sum([]byte(appSecret + sortedFields + appSecret))
	return strings.ToUpper(hex.EncodeToString(digest[:]))
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
func isSourceID(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
func isOrderID(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
