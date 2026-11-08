// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package braintree reads Braintree transactions in Dex Flows.
package braintree

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures an HTTP dependency without storing it in a connection.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable provider settings and resolves credentials per call.
type Client struct {
	baseURL          string
	apiVersion       string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration and required dependencies without contacting Braintree.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Braintree credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if parsed, err := time.Parse("2006-01-02", config.APIVersion); err != nil || parsed.Format("2006-01-02") != config.APIVersion {
		return nil, errors.New("Braintree API version must be YYYY-MM-DD")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Braintree API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, apiVersion: config.APIVersion, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Braintree client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Braintree credential unavailable")
var errRequest = errors.New("Braintree request failed")

type providerResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

const getTransactionGraphQL = "query GetTransaction($id: ID!) { node(id: $id) { __typename ... on Transaction { id status } } }"

func (client *Client) get(call sdkgo.Call, input GetTransactionInput) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	publicKey := credentials.PublicKey
	privateKey := credentials.PrivateKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(publicKey) || strings.Contains(publicKey, ":") || !providerhttp.IsHeaderSafeCredential(privateKey) {
		return providerResponse{err: errCredential}
	}
	body, err := json.Marshal(struct {
		Query     string `json:"query"`
		Variables struct {
			ID string `json:"id"`
		} `json:"variables"`
	}{Query: getTransactionGraphQL, Variables: struct {
		ID string `json:"id"`
	}{ID: input.TransactionID}})
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Braintree-Version", client.apiVersion)
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(publicKey+":"+privateKey)))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: responseBody, status: response.StatusCode, retryAfter: retryAfter, err: err}
}
