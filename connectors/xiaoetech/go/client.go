// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package xiaoetech reads XiaoeTech goods categories through its Open API.
package xiaoetech

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies an HTTP client for a proxy or provider test. Redirects are disabled.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client owns immutable configuration and a synchronized short-lived access token cache.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	tokenMu          sync.Mutex
	accessToken      string
	tokenExpiresAt   time.Time
	credentialsHash  [32]byte
}

// New validates configuration without contacting XiaoeTech. Rotated credentials take effect on the next call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("XiaoeTech credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 || config.MaxResponseBytes > 16<<20 {
		return nil, errors.New("XiaoeTech timeout or response limit is invalid")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("XiaoeTech base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("XiaoeTech client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListRootGoodsCategories returns the root goods-category Query bound to this client.
func (client *Client) ListRootGoodsCategories() ListRootGoodsCategoriesOperation {
	return ListRootGoodsCategoriesOperation{client: client}
}

func (client *Client) getAccessToken(ctx context.Context, credentials Credentials) (string, *providerFailure) {
	fingerprint := sha256.Sum256([]byte(credentials.AppID + "\x00" + credentials.ClientID + "\x00" + credentials.SecretKey.Reveal()))
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	if client.credentialsHash == fingerprint && client.accessToken != "" && time.Until(client.tokenExpiresAt) > 5*time.Minute {
		return client.accessToken, nil
	}
	// XiaoeTech documents the token request only as GET: https://api-doc.xiaoe-tech.com/api_list/get_access_token.html
	values := url.Values{"app_id": {credentials.AppID}, "client_id": {credentials.ClientID}, "secret_key": {credentials.SecretKey.Reveal()}, "grant_type": {"client_credential"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/token?"+values.Encode(), nil)
	if err != nil {
		return "", &providerFailure{branch: ListRootGoodsCategoriesBranchDefect, kind: sdkgo.FailureValidation, message: "token request is invalid"}
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", &providerFailure{kind: sdkgo.FailureAvailability, message: "XiaoeTech token request failed", retry: true}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return "", &providerFailure{kind: sdkgo.FailureRateLimit, message: "XiaoeTech token request is rate limited", retry: true, delay: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return "", &providerFailure{kind: sdkgo.FailureAvailability, message: "XiaoeTech token service is unavailable", retry: true}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return "", classifyReadError(err)
	}
	if response.StatusCode != http.StatusOK {
		return "", &providerFailure{branch: ListRootGoodsCategoriesBranchProviderRejected, kind: sdkgo.FailureAuthentication, message: "XiaoeTech rejected token request with HTTP " + strconv.Itoa(response.StatusCode)}
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return "", &providerFailure{branch: ListRootGoodsCategoriesBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "XiaoeTech returned invalid token data"}
	}
	if *wire.Code != 0 {
		return "", classifyProviderCode(*wire.Code, false)
	}
	if wire.Data == nil || !isValidSecret(wire.Data.AccessToken) || wire.Data.ExpiresIn < 600 || wire.Data.ExpiresIn > 86400 {
		return "", &providerFailure{branch: ListRootGoodsCategoriesBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "XiaoeTech returned invalid token data"}
	}
	client.accessToken = wire.Data.AccessToken
	client.tokenExpiresAt = time.Now().Add(time.Duration(wire.Data.ExpiresIn) * time.Second)
	client.credentialsHash = fingerprint
	return client.accessToken, nil
}
func (client *Client) invalidateAccessToken() {
	client.tokenMu.Lock()
	client.accessToken = ""
	client.tokenMu.Unlock()
}
