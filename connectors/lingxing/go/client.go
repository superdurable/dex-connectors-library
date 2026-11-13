// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package lingxing lists authorized Amazon store metadata through Lingxing ERP OpenAPI.
package lingxing

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for provider tests or a trusted proxy. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns Lingxing configuration and an in-memory access token. It is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	tokenMutex       sync.Mutex
	token            accessTokenCache
}
type accessTokenCache struct {
	value           string
	credentialsHash [32]byte
	expiresAt       time.Time
}

// New validates configuration without contacting Lingxing. Rotated credentials invalidate the cached token.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Lingxing credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, errors.New("Lingxing timeout and response limit must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Lingxing base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Lingxing client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListStores returns the authorized-store Query bound to this client.
func (client *Client) ListStores() ListStoresOperation { return ListStoresOperation{client: client} }

func (client *Client) getAccessToken(call sdkgo.Call, credentials Credentials) (string, *providerIssue) {
	appID := credentials.AppID.Reveal()
	appSecret := credentials.AppSecret.Reveal()
	hash := sha256.Sum256([]byte(appID + "\x00" + appSecret))
	client.tokenMutex.Lock()
	defer client.tokenMutex.Unlock()
	if client.token.value != "" && client.token.credentialsHash == hash && time.Now().Before(client.token.expiresAt) {
		return client.token.value, nil
	}
	var body strings.Builder
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("appId", appID); err != nil {
		return "", &providerIssue{branch: ListStoresBranchDefect, kind: sdkgo.FailureValidation, message: "Lingxing token request is invalid"}
	}
	if err := writer.WriteField("appSecret", appSecret); err != nil {
		return "", &providerIssue{branch: ListStoresBranchDefect, kind: sdkgo.FailureValidation, message: "Lingxing token request is invalid"}
	}
	if err := writer.Close(); err != nil {
		return "", &providerIssue{branch: ListStoresBranchDefect, kind: sdkgo.FailureValidation, message: "Lingxing token request is invalid"}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/api/auth-server/oauth/access-token", strings.NewReader(body.String()))
	if err != nil {
		return "", &providerIssue{branch: ListStoresBranchDefect, kind: sdkgo.FailureValidation, message: "Lingxing token request is invalid"}
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", &providerIssue{retry: true, kind: sdkgo.FailureAvailability, message: "Lingxing token request failed"}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return "", &providerIssue{retry: true, kind: sdkgo.FailureRateLimit, message: "Lingxing token request was rate limited", delay: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return "", &providerIssue{retry: true, kind: sdkgo.FailureAvailability, message: "Lingxing token service is unavailable"}
	}
	data, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return "", &providerIssue{branch: ListStoresBranchInvalidResponse, kind: sdkgo.FailureResponseTooLarge, message: "Lingxing token response exceeded the configured limit"}
	}
	if err != nil {
		return "", &providerIssue{retry: true, kind: sdkgo.FailureAvailability, message: "Lingxing token response was interrupted"}
	}
	if response.StatusCode != http.StatusOK {
		return "", &providerIssue{branch: ListStoresBranchProviderRejected, kind: classifyStoreHTTP(response.StatusCode), message: "Lingxing rejected token request"}
	}
	var wire struct {
		Code json.RawMessage `json:"code"`
		Data *struct {
			Token     string          `json:"access_token"`
			ExpiresIn json.RawMessage `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return "", &providerIssue{branch: ListStoresBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "Lingxing token response was invalid"}
	}
	code, ok := parseProviderCode(wire.Code)
	if !ok {
		return "", &providerIssue{branch: ListStoresBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "Lingxing token status was invalid"}
	}
	if code != 200 {
		return "", &providerIssue{branch: ListStoresBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "Lingxing rejected AppID or AppSecret"}
	}
	if wire.Data == nil || !validToken(wire.Data.Token) {
		return "", &providerIssue{branch: ListStoresBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "Lingxing token response was incomplete"}
	}
	expiresIn, validExpiry := parseProviderCode(wire.Data.ExpiresIn)
	if !validExpiry || expiresIn < 60 || expiresIn > 86400 {
		return "", &providerIssue{branch: ListStoresBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "Lingxing token response was incomplete"}
	}
	client.token = accessTokenCache{value: wire.Data.Token, credentialsHash: hash, expiresAt: time.Now().Add(time.Duration(expiresIn-30) * time.Second)}
	return wire.Data.Token, nil
}

type providerIssue struct {
	branch  sdkgo.BranchID
	kind    sdkgo.FailureKind
	message string
	retry   bool
	delay   time.Duration
}
