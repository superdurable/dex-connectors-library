// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package onenet reads OneNET AIoT device metadata in Dex Flows.
package onenet

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
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

// New validates OneNET configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("OneNET credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("OneNET API URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("OneNET API URL must be a host root without a path")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("OneNET client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("OneNET credential unavailable")
var errRequest = errors.New("OneNET request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) queryDevice(call sdkgo.Call, input QueryDeviceDetailInput) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	if !validDecimalID(credentials.UserID) || !validResourceID(credentials.ProductID) || !validDeviceName(input.DeviceName) {
		return providerResponse{err: errCredential}
	}
	key, err := base64.StdEncoding.Strict().DecodeString(credentials.AccessKey.Reveal())
	if err != nil || len(key) == 0 {
		return providerResponse{err: errCredential}
	}
	authorization := signedAuthorization(credentials.UserID, key, time.Now().Add(time.Hour).Unix())
	query := url.Values{"product_id": {credentials.ProductID}, "device_name": {input.DeviceName}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/device/detail?"+query.Encode(), nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", authorization)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
func signedAuthorization(userID string, key []byte, expiresAt int64) string {
	expiry := strconv.FormatInt(expiresAt, 10)
	resource := "userid/" + userID
	signatureInput := expiry + "\nsha256\n" + resource + "\n2022-05-01"
	digest := hmac.New(sha256.New, key)
	_, _ = digest.Write([]byte(signatureInput))
	signature := base64.StdEncoding.EncodeToString(digest.Sum(nil))
	return "version=2022-05-01&res=" + url.QueryEscape(resource) + "&et=" + expiry + "&method=sha256&sign=" + url.QueryEscape(signature)
}
func validDecimalID(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
func validResourceID(value string) bool {
	if len(value) == 0 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
func validDeviceName(value string) bool {
	if len(value) == 0 || len(value) > 256 || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
