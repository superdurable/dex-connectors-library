// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package kingsoftoffice lists WPS 365 enterprise file metadata through OpenAPI.
package kingsoftoffice

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a nonserializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for a proxy or test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns WPS configuration and an in-memory application token cache. It is safe for concurrent Steps.
type Client struct {
	baseURL               string
	pageSize              int64
	maxResponseBytes      int64
	requestSigningEnabled bool
	credentials           CredentialSource
	httpClient            *http.Client
	tokenMu               sync.Mutex
	tokenFingerprint      [32]byte
	accessToken           string
	accessTokenExpires    time.Time
}

// New validates configuration without contacting WPS. Rotated credentials invalidate the token cache on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("WPS credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.PageSize < 1 || config.PageSize > 500 {
		return nil, errors.New("WPS timeout, maxResponseBytes, or pageSize is invalid")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("WPS base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, pageSize: config.PageSize, maxResponseBytes: config.MaxResponseBytes, requestSigningEnabled: config.RequestSigningEnabled, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("WPS client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListFolderFiles returns the one-page file metadata Query bound to this client.
func (client *Client) ListFolderFiles() ListFolderFilesOperation {
	return ListFolderFilesOperation{client: client}
}

type accessTokenFailure struct {
	kind            sdkgo.FailureKind
	message         string
	retry           bool
	invalidResponse bool
	delay           time.Duration
}

func (client *Client) accessTokenFor(ctx context.Context, appID, appKey string) (string, *accessTokenFailure) {
	fingerprint := sha256.Sum256([]byte(appID + ":" + appKey))
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	if client.accessToken != "" && client.tokenFingerprint == fingerprint && time.Now().Add(5*time.Minute).Before(client.accessTokenExpires) {
		return client.accessToken, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {appID}, "client_secret": {appKey}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", &accessTokenFailure{kind: sdkgo.FailureValidation, message: "WPS token request is invalid"}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", &accessTokenFailure{kind: sdkgo.FailureAvailability, message: "WPS token request failed", retry: true}
	}
	defer func() { _ = response.Body.Close() }()
	delay := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return "", &accessTokenFailure{kind: sdkgo.FailureRateLimit, message: "WPS token request was rate limited", retry: true, delay: delay}
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return "", &accessTokenFailure{kind: sdkgo.FailureAvailability, message: "WPS token service is unavailable", retry: true, delay: delay}
	}
	if response.StatusCode != http.StatusOK {
		return "", &accessTokenFailure{kind: classifyWPSHTTPStatus(response.StatusCode), message: "WPS rejected application credentials"}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return "", &accessTokenFailure{kind: sdkgo.FailureResponseTooLarge, message: "WPS token response exceeded limit", invalidResponse: true}
	}
	if err != nil {
		return "", &accessTokenFailure{kind: sdkgo.FailureAvailability, message: "WPS token response was interrupted", retry: true}
	}
	var wire struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
		Code        int    `json:"code"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return "", &accessTokenFailure{kind: sdkgo.FailureProtocol, message: "WPS token response was malformed", invalidResponse: true}
	}
	if wire.Code != 0 {
		return "", &accessTokenFailure{kind: sdkgo.FailureAuthentication, message: "WPS rejected application credentials"}
	}
	if wire.AccessToken == "" || wire.ExpiresIn < 360 || wire.ExpiresIn > 86400 || !strings.EqualFold(wire.TokenType, "bearer") || strings.ContainsAny(wire.AccessToken, " \r\n\t") {
		return "", &accessTokenFailure{kind: sdkgo.FailureProtocol, message: "WPS token response was invalid", invalidResponse: true}
	}
	client.tokenFingerprint = fingerprint
	client.accessToken = wire.AccessToken
	client.accessTokenExpires = time.Now().Add(time.Duration(wire.ExpiresIn) * time.Second)
	return client.accessToken, nil
}
