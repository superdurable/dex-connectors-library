// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package rabbitmq inspects queue metrics through RabbitMQ's Management API.
package rabbitmq

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP transport dependency.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport for provider tests or a private cluster.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Management API settings and resolves credentials per call.
type Client struct {
	managementURL    string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates the Management API URL and required dependencies without contacting RabbitMQ.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("RabbitMQ credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	managementURL, err := providerhttp.ValidateBaseURL(config.ManagementURL)
	if err != nil {
		return nil, fmt.Errorf("RabbitMQ Management API URL: %w", err)
	}
	parsedURL, err := url.Parse(managementURL)
	if err != nil || parsedURL.Path != "" {
		return nil, errors.New("RabbitMQ Management API URL must be an origin without a path")
	}
	client := &Client{managementURL: managementURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("RabbitMQ client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("RabbitMQ credentials are unavailable")
var errRequest = errors.New("RabbitMQ request failed")

type providerResponse struct {
	status     int
	body       []byte
	retryAfter time.Duration
	err        error
}

func (client *Client) getQueue(call sdkgo.Call, virtualHost, name string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.Username) || strings.Contains(credentials.Username, ":") || !providerhttp.IsHeaderSafeCredential(credentials.Password.Reveal()) {
		return providerResponse{err: errCredential}
	}
	endpoint := client.managementURL + "/api/queues/" + url.PathEscape(virtualHost) + "/" + url.PathEscape(name)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.SetBasicAuth(credentials.Username, credentials.Password.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{status: response.StatusCode, body: body, retryAfter: retryAfter, err: err}
}

func validQueueIdentity(value string) bool {
	if value == "" || len(value) > 255 || !utf8.ValidString(value) {
		return false
	}
	for _, point := range value {
		if unicode.IsControl(point) {
			return false
		}
	}
	return true
}

func queueFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "rabbitmq", Operation: "getQueueMetrics", Message: message}
}

func queueBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[QueueMetrics] {
	failure := queueFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, QueueMetrics{}, &failure, sdkgo.Receipt{})
}

func rejectedStatus(status int) string { return "RabbitMQ rejected HTTP " + strconv.Itoa(status) }
