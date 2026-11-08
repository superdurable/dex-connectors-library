// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package box downloads bounded files and replaces existing content through typed Dex Steps.
package box

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies an HTTP dependency without storing it in a connection.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Box HTTP settings and resolves OAuth credentials per call.
type Client struct {
	baseURL          string
	uploadBaseURL    string
	maxResponseBytes int64
	maxDownloadBytes int64
	maxUploadBytes   int64
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	httpClient       *http.Client
}

// New validates configuration without contacting Box.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Box credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxUploadBytes > 50*1024*1024 {
		return nil, errors.New("Box direct upload limit exceeds 50 MiB")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Box API URL: %w", err)
	}
	uploadBaseURL, err := providerhttp.ValidateBaseURL(config.UploadBaseURL)
	if err != nil {
		return nil, fmt.Errorf("Box upload API URL: %w", err)
	}
	client := &Client{baseURL: baseURL, uploadBaseURL: uploadBaseURL, maxResponseBytes: config.MaxResponseBytes, maxDownloadBytes: config.MaxDownloadBytes, maxUploadBytes: config.MaxUploadBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Box client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.refreshDriver = NewCredentialRefreshDriver(client.httpClient)
	return client, nil
}

var errCredential = errors.New("Box credential unavailable")
var errRefresh = errors.New("Box credential refresh failed")
var errRequest = errors.New("Box request failed")

type fileResponse struct {
	body        []byte
	status      int
	retryAfter  time.Duration
	requestSent bool
	err         error
}

func (client *Client) resolveAccessToken(call sdkgo.Call) (string, error) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if sdkgo.IsReauthorizationRequired(err) {
		return "", errCredential
	}
	if err != nil {
		return "", errRefresh
	}
	if credentials.Validate() != nil || validateResolvedCredentials(credentials) != nil {
		return "", errCredential
	}
	return credentials.AccessToken.Reveal(), nil
}

func (client *Client) download(call sdkgo.Call, fileID string) fileResponse {
	accessToken, err := client.resolveAccessToken(call)
	if err != nil {
		return fileResponse{err: err}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/2.0/files/"+url.PathEscape(fileID)+"/content", nil)
	if err != nil {
		return fileResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fileResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	limit := client.maxDownloadBytes
	if response.StatusCode != http.StatusOK {
		limit = client.maxResponseBytes
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, limit)
	if bytes.Contains(body, []byte(accessToken)) {
		return fileResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return fileResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func queryFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "box", Operation: operation, Message: message}
}

func queryBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
