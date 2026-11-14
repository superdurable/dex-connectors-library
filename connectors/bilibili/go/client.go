// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package bilibili reads authorized account identity through the official Open Platform.
package bilibili

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// Client holds immutable provider settings and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates Bilibili configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Bilibili credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Bilibili API URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, errors.New("Bilibili API URL must be an origin without a path or query")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Bilibili client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Bilibili credentials unavailable")
var errRequest = errors.New("Bilibili request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) getAuthorizedUser(call sdkgo.Call) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	clientID, appSecret, accessToken := credentials.ClientID, credentials.AppSecret.Reveal(), credentials.AccessToken.Reveal()
	if !isOpaqueCredential(clientID) || !isOpaqueCredential(appSecret) || !isOpaqueCredential(accessToken) {
		return providerResponse{err: errCredential}
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return providerResponse{err: errRequest}
	}
	headers := map[string]string{
		"x-bili-accesskeyid":       clientID,
		"x-bili-content-md5":       fmt.Sprintf("%x", md5.Sum(nil)),
		"x-bili-signature-method":  "HMAC-SHA256",
		"x-bili-signature-nonce":   hex.EncodeToString(nonceBytes),
		"x-bili-signature-version": "2.0",
		"x-bili-timestamp":         strconv.FormatInt(time.Now().Unix(), 10),
	}
	signature := signRequest(appSecret, headers)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/arcopen/fn/user/account/info", nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Access-Token", accessToken)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Authorization", signature)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}
func signRequest(appSecret string, headers map[string]string) string {
	// The signed header order is fixed by the provider's ASCII sort rule.
	keys := []string{"x-bili-accesskeyid", "x-bili-content-md5", "x-bili-signature-method", "x-bili-signature-nonce", "x-bili-signature-version", "x-bili-timestamp"}
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+":"+headers[key])
	}
	signer := hmac.New(sha256.New, []byte(appSecret))
	_, _ = signer.Write([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(signer.Sum(nil))
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
