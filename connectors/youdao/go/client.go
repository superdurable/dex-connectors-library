// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package youdao translates bounded text through Youdao's signed API.
package youdao

import (
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

// Option configures the transport without changing a connection.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies an HTTP transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable settings and resolves Youdao credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates Youdao configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Youdao credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Youdao API URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("Youdao API URL must be a host root without a path")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Youdao client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Youdao credential unavailable")
var errRequest = errors.New("Youdao request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) translate(call sdkgo.Call, input TranslateTextInput) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	appKey := credentials.AppKey
	appSecret := credentials.AppSecret.Reveal()
	if !validAppKey(appKey) || appSecret == "" {
		return providerResponse{err: errCredential}
	}
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return providerResponse{err: errRequest}
	}
	salt := hex.EncodeToString(saltBytes)
	curtime := strconv.FormatInt(time.Now().Unix(), 10)
	values := url.Values{"q": {input.Text}, "from": {input.SourceLanguage}, "to": {input.TargetLanguage}, "appKey": {appKey}, "salt": {salt}, "curtime": {curtime}, "signType": {"v3"}}
	values.Set("sign", signTranslation(appKey, input.Text, salt, curtime, appSecret))
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/api", strings.NewReader(values.Encode()))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}
func signTranslation(appKey, text, salt, curtime, appSecret string) string {
	characters := []rune(text)
	input := text
	if len(characters) > 20 {
		input = string(characters[:10]) + strconv.Itoa(len(characters)) + string(characters[len(characters)-10:])
	}
	digest := sha256.Sum256([]byte(appKey + input + salt + curtime + appSecret))
	return hex.EncodeToString(digest[:])
}

func validAppKey(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
