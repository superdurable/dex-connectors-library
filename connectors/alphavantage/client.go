// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package alphavantage exposes typed alphavantage provider Queries to Dex Flows.
package alphavantage

import (
	"bytes"
	"encoding/json"
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

// Option supplies a non-serializable HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or a provider test.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable HTTP configuration and a credential source; it is safe for concurrent use.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting the provider; credentials are resolved per call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("alphavantage credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("alphavantage base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("provider credential unavailable")
var errRequest = errors.New("provider request failed")

func (client *Client) do(call sdkgo.Call, method, path string, query url.Values, payload []byte) ([]byte, int, time.Duration, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, 0, 0, errCredential
	}
	if err = credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, 0, 0, errCredential
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("apikey", credentials.APIKey.Reveal())
	endpoint := client.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	request, err := http.NewRequestWithContext(call.Context, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, 0, errRequest
	}

	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
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

func invokeJSONQuery[T any](call sdkgo.Call, client *Client, operation string, branches queryBranches, method, path string, query url.Values, payload []byte, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	body, status, retryAfter, err := client.do(call, method, path, query, payload)
	if errors.Is(err, errCredential) {
		return queryBranch(operation, branches.defect, zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= http.StatusInternalServerError {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "provider is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound && branches.notFound != "" {
		return queryBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "provider did not find the resource")
	}
	if status != http.StatusOK {
		return queryBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, branches.invalid, zero, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	var signal map[string]json.RawMessage
	if json.Unmarshal(body, &signal) == nil {
		if _, ok := signal["Note"]; ok {
			return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureRateLimit, "Alpha Vantage rate limit notice"), 0)
		}
		if information, ok := signal["Information"]; ok {
			var message string
			_ = json.Unmarshal(information, &message)
			if strings.Contains(strings.ToLower(message), "rate limit") || strings.Contains(strings.ToLower(message), "call frequency") || strings.Contains(strings.ToLower(message), "requests per day") {
				return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureRateLimit, "Alpha Vantage rate limit notice"), 0)
			}
			return queryBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "Alpha Vantage rejected the request")
		}
		if _, ok := signal["Error Message"]; ok {
			return queryBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "Alpha Vantage rejected the request")
		}
	}
	result, err := decode(body)
	if errors.Is(err, errNoData) && branches.notFound != "" {
		return queryBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "Alpha Vantage returned no data for the symbol")
	}
	if err != nil {
		return queryBranch(operation, branches.invalid, zero, sdkgo.FailureProtocol, "provider returned an invalid response")
	}
	return sdkgo.NewQueryBranch(branches.found, result, nil, sdkgo.Receipt{})
}
func queryBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func queryFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "alphavantage", Operation: operation, Message: message}
}
