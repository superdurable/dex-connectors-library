// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package iflytek generates bounded text through the Spark HTTP API.
package iflytek

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures the HTTP transport without changing a connection.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies an HTTP transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable settings and resolves Spark credentials per call.
type Client struct {
	baseURL          string
	model            string
	maxOutputTokens  int64
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates Spark configuration without contacting the provider.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Spark credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxOutputTokens < 1 || config.MaxOutputTokens > 4096 {
		return nil, errors.New("Spark maximum output tokens must be between 1 and 4096")
	}
	if !validModel(config.Model) {
		return nil, errors.New("Spark model must be lite, generalv3, pro-128k, or 4.0Ultra")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Spark API URL: %w", err)
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("Spark API URL must be a host root without a path")
	}
	client := &Client{baseURL: baseURL, model: config.Model, maxOutputTokens: config.MaxOutputTokens, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Spark client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Spark credential unavailable")
var errRequest = errors.New("Spark request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) complete(call sdkgo.Call, input CreateChatCompletionInput) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	password := credentials.APIPassword.Reveal()
	if password == "" || strings.ContainsAny(password, "\r\n ") {
		return providerResponse{err: errCredential}
	}
	body, err := json.Marshal(struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream    bool `json:"stream"`
		MaxTokens int  `json:"max_tokens"`
	}{Model: client.model, Messages: []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{{Role: "user", Content: input.Prompt}}, Stream: false, MaxTokens: int(client.maxOutputTokens)})
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+password)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}
func validModel(model string) bool {
	return model == "lite" || model == "generalv3" || model == "pro-128k" || model == "4.0Ultra"
}
