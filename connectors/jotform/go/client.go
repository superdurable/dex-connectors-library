// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package jotform exposes typed Jotform metadata Queries to Dex Flows.
package jotform

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures an HTTP dependency without serializing it into a connection.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable provider configuration and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates connection configuration without contacting Jotform.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Jotform credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Jotform API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Jotform client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Jotform credential unavailable")
var errRequest = errors.New("Jotform request failed")

func (client *Client) get(call sdkgo.Call, path string) ([]byte, int, time.Duration, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return nil, 0, 0, errCredential
	}
	apiKey := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiKey) {
		return nil, 0, 0, errCredential
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return nil, 0, 0, errRequest
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("APIKEY", apiKey)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, 0, 0, errRequest
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return body, response.StatusCode, retryAfter, err
}

type queryBranches struct{ found, notFound, rejected, invalid, defect sdkgo.BranchID }

func invokeQuery[T any](call sdkgo.Call, client *Client, operation, path string, branches queryBranches, decode func([]byte) (T, int, error)) sdkgo.QueryAttempt[T] {
	var zero T
	body, status, retryAfter, err := client.get(call, path)
	if errors.Is(err, errCredential) {
		return queryBranch(operation, branches.defect, zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureTransport, "Jotform request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureRateLimit, "Jotform rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "Jotform is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound && branches.notFound != "" {
		return queryBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "Jotform did not find the submission")
	}
	if status != http.StatusOK {
		return queryBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "Jotform rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, branches.invalid, zero, sdkgo.FailureResponseTooLarge, "Jotform response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureTransport, "Jotform response was interrupted"), 0)
	}
	value, responseCode, err := decode(body)
	if err != nil {
		return queryBranch(operation, branches.invalid, zero, sdkgo.FailureProtocol, "Jotform returned invalid JSON")
	}
	if responseCode == 429 || responseCode >= 500 {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "Jotform reported temporary unavailability"), retryAfter)
	}
	if responseCode == 404 && branches.notFound != "" {
		return queryBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "Jotform did not find the submission")
	}
	if responseCode != 200 {
		return queryBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "Jotform rejected the request")
	}
	return sdkgo.NewQueryBranch(branches.found, value, nil, sdkgo.Receipt{})
}

func queryBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func queryFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "jotform", Operation: operation, Message: message}
}
