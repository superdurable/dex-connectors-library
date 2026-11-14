// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package weimob reads Weimob WOS store information in Dex Flows.
package weimob

import (
	"bytes"
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

// Option configures the HTTP transport without changing a connection.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable provider settings and caches one authorized store token.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	tokenMu          sync.Mutex
	tokenFingerprint [32]byte
	token            string
	tokenExpiresAt   time.Time
}

// New validates Weimob configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Weimob credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Weimob API URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("Weimob API URL must be a host root without a path")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Weimob client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Weimob credentials unavailable")
var errRequest = errors.New("Weimob request failed")
var errTokenRejected = errors.New("Weimob token request rejected")
var errTokenProtocol = errors.New("Weimob token response invalid")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) getStoreBasic(call sdkgo.Call) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	clientID := credentials.ClientID
	clientSecret := credentials.ClientSecret.Reveal()
	if !isOpaqueCredential(clientID) || !isOpaqueCredential(clientSecret) || !isShopID(credentials.ShopID) {
		return providerResponse{err: errCredential}
	}
	token, tokenResponse := client.resolveAccessToken(call, clientID, clientSecret, credentials.ShopID)
	if tokenResponse.err != nil || tokenResponse.status != 0 {
		return tokenResponse
	}
	// Weimob API gateway requires accesstoken in the query: https://doc.weimobcloud.com/detail?childMenuId=1&id=3247&isold=2&menuId=19&tag=1655
	endpoint := client.baseURL + "/apigw/bos/v2.0/info/get?accesstoken=" + url.QueryEscape(token)
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, bytes.NewBufferString("{}"))
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

func (client *Client) resolveAccessToken(call sdkgo.Call, clientID string, clientSecret string, shopID string) (string, providerResponse) {
	fingerprint := sha256.Sum256([]byte(clientID + "\x00" + clientSecret + "\x00" + shopID))
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	if client.token != "" && client.tokenFingerprint == fingerprint && time.Until(client.tokenExpiresAt) > time.Minute {
		return client.token, providerResponse{}
	}
	// Weimob documents token parameters only in the URL query: https://doc.weimobcloud.com/word?childMenuId=54&isold=2&menuId=53&tag=3692
	parameters := url.Values{"grant_type": {"client_credentials"}, "client_id": {clientID}, "client_secret": {clientSecret}}
	if shopID != "" {
		parameters.Set("shop_id", shopID)
		parameters.Set("shop_type", "business_operation_system_id")
	}
	endpoint := client.baseURL + "/fuwu/b/oauth2/token?" + parameters.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, bytes.NewBufferString("{}"))
	if err != nil {
		return "", providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return "", providerResponse{status: response.StatusCode, retryAfter: retryAfter}
	}
	if response.StatusCode != http.StatusOK {
		return "", providerResponse{err: errTokenRejected}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, 8192)
	if err != nil {
		return "", providerResponse{err: errTokenProtocol}
	}
	var wire struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return "", providerResponse{err: errTokenProtocol}
	}
	if wire.Error != "" {
		return "", providerResponse{err: errTokenRejected}
	}
	if !isOpaqueCredential(wire.AccessToken) || wire.ExpiresIn < 120 || wire.ExpiresIn > 604800 {
		return "", providerResponse{err: errTokenProtocol}
	}
	client.token = wire.AccessToken
	client.tokenFingerprint = fingerprint
	client.tokenExpiresAt = time.Now().Add(time.Duration(wire.ExpiresIn) * time.Second)
	return client.token, providerResponse{}
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
func isShopID(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 20 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
