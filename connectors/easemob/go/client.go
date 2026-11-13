// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package easemob reads IM user account details in Dex Flows.
package easemob

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

// Client holds provider settings and a synchronized short-lived App Token cache.
type Client struct {
	baseURL                    string
	maxResponseBytes           int64
	credentials                CredentialSource
	httpClient                 *http.Client
	tokenMutex                 sync.Mutex
	token                      string
	tokenExpiresAt             time.Time
	tokenCredentialFingerprint [32]byte
}

// New validates Easemob configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Easemob credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Easemob REST URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("Easemob REST URL must be a host root without a path")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Easemob client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Easemob credential unavailable")
var errRequest = errors.New("Easemob request failed")
var errInvalidTokenResponse = errors.New("Easemob invalid token response")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) getUser(call sdkgo.Call, input GetUserDetailInput) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	organization, application, isValid := parseAppKey(credentials.AppKey)
	if !isValid || !validClientID(credentials.ClientID) || credentials.ClientSecret.Reveal() == "" || !validUserID(input.UserID) {
		return providerResponse{err: errCredential}
	}
	tokenResponse := client.appToken(call, credentials, organization, application, false)
	if tokenResponse.err != nil || tokenResponse.status != http.StatusOK {
		return tokenResponse
	}
	response := client.requestUser(call, organization, application, input.UserID, string(tokenResponse.body))
	if response.status != http.StatusUnauthorized {
		return response
	}
	tokenResponse = client.appToken(call, credentials, organization, application, true)
	if tokenResponse.err != nil || tokenResponse.status != http.StatusOK {
		return tokenResponse
	}
	return client.requestUser(call, organization, application, input.UserID, string(tokenResponse.body))
}
func (client *Client) appToken(call sdkgo.Call, credentials Credentials, organization, application string, forceRefresh bool) providerResponse {
	client.tokenMutex.Lock()
	defer client.tokenMutex.Unlock()
	fingerprint := sha256.Sum256([]byte(credentials.AppKey + "\x00" + credentials.ClientID + "\x00" + credentials.ClientSecret.Reveal()))
	if !forceRefresh && client.token != "" && fingerprint == client.tokenCredentialFingerprint && time.Now().Before(client.tokenExpiresAt) {
		return providerResponse{body: []byte(client.token), status: http.StatusOK}
	}
	body, err := json.Marshal(map[string]string{"grant_type": "client_credentials", "client_id": credentials.ClientID, "client_secret": credentials.ClientSecret.Reveal()})
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/"+organization+"/"+application+"/token", bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response := client.doRequest(request)
	if response.err != nil || response.status != http.StatusOK {
		return response
	}
	var wire struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.AccessToken == "" || wire.ExpiresIn <= 0 || wire.ExpiresIn > 365*24*3600 || strings.ContainsAny(wire.AccessToken, " \t\r\n") {
		return providerResponse{err: errInvalidTokenResponse}
	}
	expiresIn := time.Duration(wire.ExpiresIn) * time.Second
	safetyMargin := min(30*time.Second, expiresIn/10)
	client.token = wire.AccessToken
	client.tokenExpiresAt = time.Now().Add(expiresIn - safetyMargin)
	client.tokenCredentialFingerprint = fingerprint
	return providerResponse{body: []byte(wire.AccessToken), status: http.StatusOK}
}
func (client *Client) requestUser(call sdkgo.Call, organization, application, userID, token string) providerResponse {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/"+organization+"/"+application+"/users/"+userID, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	return client.doRequest(request)
}
func (client *Client) doRequest(request *http.Request) providerResponse {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
func parseAppKey(value string) (string, string, bool) {
	parts := strings.Split(value, "#")
	if len(parts) != 2 || !validPathSegment(parts[0]) || !validPathSegment(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}
func validPathSegment(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
func validClientID(value string) bool {
	return len(value) > 0 && len(value) <= 256 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n")
}
func validUserID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.') {
			return false
		}
	}
	return true
}
