// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package dropbox exposes typed Dropbox file Queries to Dex Flows.
package dropbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures an HTTP dependency without persisting it in a connection.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable provider configuration and resolves OAuth credentials per call.
type Client struct {
	baseURL          string
	contentBaseURL   string
	maxResponseBytes int64
	maxDownloadBytes int64
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	httpClient       *http.Client
}

// New validates connection configuration without contacting Dropbox.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Dropbox credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Dropbox API URL: %w", err)
	}
	contentBaseURL, err := providerhttp.ValidateBaseURL(config.ContentBaseURL)
	if err != nil {
		return nil, fmt.Errorf("Dropbox content API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, contentBaseURL: contentBaseURL, maxResponseBytes: config.MaxResponseBytes, maxDownloadBytes: config.MaxDownloadBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Dropbox client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.refreshDriver = NewCredentialRefreshDriver(client.httpClient)
	return client, nil
}

var errCredential = errors.New("Dropbox credential unavailable")
var errRefresh = errors.New("Dropbox credential refresh failed")
var errRequest = errors.New("Dropbox request failed")

func (client *Client) post(call sdkgo.Call, path string, payload []byte) ([]byte, int, time.Duration, error) {
	accessToken, err := client.resolveAccessToken(call)
	if err != nil {
		return nil, 0, 0, err
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, 0, errRequest
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, 0, 0, errRequest
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return nil, response.StatusCode, retryAfter, errCredential
	}
	return body, response.StatusCode, retryAfter, err
}

func (client *Client) resolveAccessToken(call sdkgo.Call) (string, error) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if sdkgo.IsReauthorizationRequired(err) {
		return "", errCredential
	}
	if err != nil {
		return "", errRefresh
	}
	if credentials.Validate() != nil {
		return "", errCredential
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return "", errCredential
	}
	return accessToken, nil
}

type contentResponse struct {
	body       []byte
	metadata   string
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) download(call sdkgo.Call, filePath string) contentResponse {
	accessToken, err := client.resolveAccessToken(call)
	if err != nil {
		return contentResponse{err: err}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.contentBaseURL+"/2/files/download", nil)
	if err != nil {
		return contentResponse{err: errRequest}
	}
	request.Header.Set("Dropbox-API-Arg", `{"path":`+strconv.QuoteToASCII(filePath)+`}`)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return contentResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	limit := client.maxDownloadBytes
	if response.StatusCode != http.StatusOK {
		limit = client.maxResponseBytes
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, limit)
	metadata := response.Header.Get("Dropbox-API-Result")
	if len(metadata) > 65536 || strings.Contains(metadata, accessToken) || bytes.Contains(body, []byte(accessToken)) {
		return contentResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return contentResponse{body: body, metadata: metadata, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

type queryBranches struct{ found, notFound, cursorExpired, rejected, invalid, defect sdkgo.BranchID }

func invokeQuery[T any](call sdkgo.Call, client *Client, operation, path string, payload []byte, branches queryBranches, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	body, status, retryAfter, err := client.post(call, path, payload)
	if errors.Is(err, errCredential) {
		return queryBranch(operation, branches.defect, zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRefresh) {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "Dropbox credentials could not be refreshed"), 0)
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureTransport, "Dropbox request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureRateLimit, "Dropbox rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "Dropbox is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusConflict {
		var providerError struct {
			ErrorSummary string `json:"error_summary"`
		}
		if json.Unmarshal(body, &providerError) == nil && branches.notFound != "" && strings.HasPrefix(providerError.ErrorSummary, "path/not_found") {
			return queryBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "Dropbox did not find the resource")
		}
		if branches.cursorExpired != "" && strings.HasPrefix(providerError.ErrorSummary, "reset/") {
			return queryBranch(operation, branches.cursorExpired, zero, sdkgo.FailureProviderRejection, "Dropbox folder cursor expired")
		}
	}
	if status != http.StatusOK {
		return queryBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "Dropbox rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, branches.invalid, zero, sdkgo.FailureResponseTooLarge, "Dropbox response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureTransport, "Dropbox response was interrupted"), 0)
	}
	value, err := decode(body)
	if err != nil {
		return queryBranch(operation, branches.invalid, zero, sdkgo.FailureProtocol, "Dropbox returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(branches.found, value, nil, sdkgo.Receipt{})
}

func queryBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func queryFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "dropbox", Operation: operation, Message: message}
}
