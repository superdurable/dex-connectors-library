// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package upyun reads UPYUN object storage service usage in Dex Flows.
package upyun

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
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

// Client holds immutable service settings and resolves credentials for every call.
type Client struct {
	baseURL          string
	bucket           string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

var bucketName = regexp.MustCompile(`^[a-z][a-z0-9-]{4,19}$`)

// New validates the storage service settings without contacting UPYUN.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("UPYUN credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !bucketName.MatchString(config.Bucket) {
		return nil, errors.New("UPYUN bucket must be 5–20 lowercase letters, digits or hyphens and start with a letter")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("UPYUN API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, bucket: config.Bucket, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("UPYUN client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("UPYUN credential unavailable")
var errRequest = errors.New("UPYUN request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) getUsage(call sdkgo.Call) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	if !validOperator(credentials.Operator) {
		return providerResponse{err: errCredential}
	}
	password := credentials.Password.Reveal()
	if password == "" {
		return providerResponse{err: errCredential}
	}
	uri := "/" + client.bucket + "/?usage"
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+uri, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	date := time.Now().UTC().Format(http.TimeFormat)
	request.Header.Set("Date", date)
	request.Header.Set("Authorization", makeAuthorization(credentials.Operator, password, request.Method, request.URL.RequestURI(), date))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func validOperator(operator string) bool {
	if operator == "" || strings.TrimSpace(operator) != operator || len(operator) > 128 {
		return false
	}
	for _, character := range operator {
		if character <= 0x20 || character >= 0x7f || character == ':' {
			return false
		}
	}
	return true
}

func makeAuthorization(operator, password, method, uri, date string) string {
	passwordDigest := md5.Sum([]byte(password))
	key := make([]byte, hex.EncodedLen(len(passwordDigest)))
	hex.Encode(key, passwordDigest[:])
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write([]byte(method + "&" + uri + "&" + date))
	return "UPYUN " + operator + ":" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
