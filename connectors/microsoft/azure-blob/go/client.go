// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package azureblob downloads bounded Azure Storage blobs for Dex Flows.
package azureblob

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const storageVersion = "2023-11-03"

var accountNamePattern = regexp.MustCompile(`^[a-z0-9]{3,24}$`)

// Option configures the provider HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for production or provider tests.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

type signingTimeOption struct{ now func() time.Time }

func (option signingTimeOption) applyClientOption(client *Client) { client.now = option.now }

// Client holds an Azure account endpoint and resolves its Shared Key per call.
type Client struct {
	endpoint         string
	accountName      string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	now              func() time.Time
}

// New validates the account, endpoint, limits, and dependencies without contacting Azure.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Azure Blob credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !accountNamePattern.MatchString(config.AccountName) {
		return nil, errors.New("Azure storage account name is invalid")
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://" + config.AccountName + ".blob.core.windows.net"
	}
	validated, err := providerhttp.ValidateBaseURL(endpoint)
	if err != nil {
		return nil, fmt.Errorf("Azure Blob endpoint: %w", err)
	}
	client := &Client{endpoint: strings.TrimRight(validated, "/"), accountName: config.AccountName, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Azure Blob client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Azure Blob credential unavailable")
var errRequest = errors.New("Azure Blob request failed")

type providerResponse struct {
	body        []byte
	status      int
	contentType string
	etag        string
	retryAfter  time.Duration
	err         error
}

func (client *Client) downloadBlob(call sdkgo.Call, container, blobName string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	accountKey := credentials.AccountKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accountKey) {
		return providerResponse{err: errCredential}
	}
	signingKey, err := base64.StdEncoding.DecodeString(accountKey)
	if err != nil || len(signingKey) == 0 {
		return providerResponse{err: errCredential}
	}
	segments := strings.Split(blobName, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	path := "/" + container + "/" + strings.Join(segments, "/")
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.endpoint+path, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	timestamp := client.now().UTC().Format(http.TimeFormat)
	canonicalResource := "/" + client.accountName + "/" + container + "/" + blobName
	signedValue := "GET" + strings.Repeat("\n", 12) + "x-ms-date:" + timestamp + "\n" + "x-ms-version:" + storageVersion + "\n" + canonicalResource
	mac := hmac.New(sha256.New, signingKey)
	if _, err := mac.Write([]byte(signedValue)); err != nil {
		return providerResponse{err: errRequest}
	}
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	request.Header.Set("x-ms-date", timestamp)
	request.Header.Set("x-ms-version", storageVersion)
	request.Header.Set("Authorization", "SharedKey "+client.accountName+":"+signature)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), client.now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accountKey)) {
		return providerResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return providerResponse{body: body, status: response.StatusCode, contentType: response.Header.Get("Content-Type"), etag: response.Header.Get("ETag"), retryAfter: retryAfter, err: err}
}
