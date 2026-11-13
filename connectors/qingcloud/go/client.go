// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package qingcloud lists accessible QingCloud IaaS zones in Dex Flows.
package qingcloud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures the provider HTTP transport without changing a connection.
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

// New validates QingCloud configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("QingCloud credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("QingCloud API URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("QingCloud API URL must be a host root without a path")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("QingCloud client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("QingCloud credential unavailable")
var errRequest = errors.New("QingCloud request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) describeZones(call sdkgo.Call) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	accessKeyID := credentials.AccessKeyID
	if !validAccessKeyID(accessKeyID) {
		return providerResponse{err: errCredential}
	}
	secretAccessKey := credentials.SecretAccessKey.Reveal()
	if secretAccessKey == "" {
		return providerResponse{err: errCredential}
	}
	params := url.Values{
		"access_key_id": {accessKeyID}, "action": {"DescribeZones"}, "limit": {"100"},
		"signature_method": {"HmacSHA256"}, "signature_version": {"1"},
		"time_stamp": {time.Now().UTC().Format("2006-01-02T15:04:05Z")}, "version": {"1"},
	}
	canonicalQuery := params.Encode()
	params.Set("signature", signCanonicalQuery(secretAccessKey, http.MethodGet, "/iaas/", canonicalQuery))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/iaas/?"+params.Encode(), nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
func validAccessKeyID(value string) bool {
	if len(value) == 0 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func signCanonicalQuery(secretAccessKey, method, path, query string) string {
	mac := hmac.New(sha256.New, []byte(secretAccessKey))
	_, _ = mac.Write([]byte(method + "\n" + path + "\n" + query))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
