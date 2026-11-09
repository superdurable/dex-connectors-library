// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package acrobatsign reads selected Adobe Acrobat Sign agreement status in Dex Flows.
package acrobatsign

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

// Client holds immutable provider settings and caches the token-specific regional API access point.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	mutex            sync.Mutex
	cachedTokenHash  [sha256.Size]byte
	cachedAccessURL  string
}

// New validates configuration and required dependencies without contacting Acrobat Sign.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Acrobat Sign credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Acrobat Sign discovery URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Acrobat Sign client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var errCredential = errors.New("Acrobat Sign credential unavailable")
var errRequest = errors.New("Acrobat Sign request failed")
var errInvalidAccessPoint = errors.New("Acrobat Sign returned an invalid API access point")

type providerResponse struct {
	body        []byte
	status      int
	retryAfter  time.Duration
	err         error
	isDiscovery bool
}

func (client *Client) get(call sdkgo.Call, path string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	integrationKey := credentials.IntegrationKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(integrationKey) {
		return providerResponse{err: errCredential}
	}
	accessPoint := client.discoverAccessPoint(call, integrationKey)
	if accessPoint.err != nil || accessPoint.status != http.StatusOK {
		return accessPoint.providerResponse
	}
	return client.getFromURL(call, accessPoint.apiURL+"/api/rest/v6"+path, integrationKey)
}

type accessPointResult struct {
	apiURL string
	providerResponse
}

func (client *Client) discoverAccessPoint(call sdkgo.Call, integrationKey string) accessPointResult {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	keyHash := sha256.Sum256([]byte(integrationKey))
	if client.cachedAccessURL != "" && client.cachedTokenHash == keyHash {
		return accessPointResult{apiURL: client.cachedAccessURL, providerResponse: providerResponse{status: http.StatusOK}}
	}
	response := client.getFromURL(call, client.baseURL+"/baseUris", integrationKey)
	response.isDiscovery = true
	if response.err != nil || response.status != http.StatusOK {
		return accessPointResult{providerResponse: response}
	}
	var wire struct {
		APIAccessPoint string `json:"apiAccessPoint"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil {
		return accessPointResult{providerResponse: providerResponse{status: http.StatusOK, isDiscovery: true, err: errInvalidAccessPoint}}
	}
	apiURL, err := validateAccessPoint(wire.APIAccessPoint, client.baseURL)
	if err != nil {
		return accessPointResult{providerResponse: providerResponse{status: http.StatusOK, isDiscovery: true, err: errInvalidAccessPoint}}
	}
	client.cachedAccessURL = apiURL
	client.cachedTokenHash = keyHash
	return accessPointResult{apiURL: apiURL, providerResponse: providerResponse{status: http.StatusOK}}
}

func (client *Client) getFromURL(call sdkgo.Call, requestURL string, integrationKey string) providerResponse {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, requestURL, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+integrationKey)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func validateAccessPoint(rawAccessPoint string, discoveryURL string) (string, error) {
	accessPoint, err := url.Parse(rawAccessPoint)
	if err != nil || accessPoint.Host == "" || accessPoint.User != nil || accessPoint.RawQuery != "" || accessPoint.Fragment != "" || (accessPoint.Path != "" && accessPoint.Path != "/") {
		return "", errInvalidAccessPoint
	}
	discovery, err := url.Parse(discoveryURL)
	if err != nil {
		return "", errInvalidAccessPoint
	}
	if isLoopbackHost(discovery.Hostname()) {
		if accessPoint.Scheme != discovery.Scheme || accessPoint.Host != discovery.Host {
			return "", errInvalidAccessPoint
		}
	} else {
		host := strings.ToLower(accessPoint.Hostname())
		if accessPoint.Scheme != "https" || accessPoint.Port() != "" || !strings.HasPrefix(host, "api.") || !(strings.HasSuffix(host, ".adobesign.com") || strings.HasSuffix(host, ".echosign.com")) {
			return "", errInvalidAccessPoint
		}
	}
	return strings.TrimSuffix(accessPoint.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
