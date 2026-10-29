// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package cloudflare exposes typed Dex Steps for the Cloudflare REST API.
package cloudflare

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

// Option supplies an HTTP transport without serializing it into a Dex Flow.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a client transport; New copies it and disables redirects.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable HTTP settings and resolves secrets per request.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Cloudflare.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Cloudflare credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Cloudflare base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

type requestSpec struct {
	method, path, contentType string
	query                     url.Values
	body                      []byte
}

// ListZonesInput requests the default Cloudflare zone page.
type ListZonesInput struct{}

// Zone identifies a Cloudflare zone.
type Zone struct {
	// ID is the zone identifier used by cache operations.
	ID string `json:"id"`
	// Name is the zone's DNS name.
	Name string `json:"name"`
	// Status is the provider-reported zone status.
	Status string `json:"status"`
}

// ZonesPage contains Cloudflare zones and success metadata.
type ZonesPage struct {
	// Success reports whether Cloudflare accepted the list request.
	Success bool `json:"success"`
	// Result contains one zone page, possibly empty.
	Result []Zone `json:"result"`
}

// PurgeCacheByURLsInput names one zone and up to 30 absolute URLs.
type PurgeCacheByURLsInput struct {
	// ZoneID identifies the zone whose cache is purged.
	ZoneID string `json:"zoneId"`
	// URLs lists absolute HTTP or HTTPS URLs to purge.
	URLs []string `json:"urls"`
}

// CachePurge records Cloudflare's acceptance, not a cache miss guarantee.
type CachePurge struct {
	// Success reports that Cloudflare accepted the purge request.
	Success bool `json:"success"`
	// Result contains Cloudflare's purge request ID.
	Result struct {
		ID string `json:"id"`
	} `json:"result"`
}

// ListZonesOperation invokes listZones with one provider request.
type ListZonesOperation struct{ client *Client }

// ListZones binds the operation to this client.
func (client *Client) ListZones() ListZonesOperation { return ListZonesOperation{client} }

// Definition returns the generated listZones contract.
func (ListZonesOperation) Definition() sdkgo.QueryDefinition { return ListZonesDefinition }

// Invoke validates input, calls Cloudflare, and classifies the response.
func (operation ListZonesOperation) Invoke(call sdkgo.Call, input ListZonesInput) sdkgo.QueryAttempt[ZonesPage] {
	spec, err := buildListZonesRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListZonesBranchDefect, ZonesPage{}, failure(sdkgo.FailureValidation, "listZones", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "listZones", spec, ListZonesBranchListed, "", ListZonesBranchProviderRejected, ListZonesBranchInvalidResponse, ListZonesBranchDefect, decodeListZones)
}
func buildListZonesRequest(input ListZonesInput) (requestSpec, error) {
	return requestSpec{method: http.MethodGet, path: "/zones"}, nil
}
func decodeListZones(body []byte) (ZonesPage, error) {
	var value ZonesPage
	if err := json.Unmarshal(body, &value); err != nil {
		return ZonesPage{}, err
	}
	if !value.Success || value.Result == nil {
		return ZonesPage{}, fmt.Errorf("missing successful zone result")
	}
	return value, nil
}

// PurgeCacheByURLsOperation invokes purgeCacheByURLs with one provider request.
type PurgeCacheByURLsOperation struct{ client *Client }

// PurgeCacheByURLs binds the operation to this client.
func (client *Client) PurgeCacheByURLs() PurgeCacheByURLsOperation {
	return PurgeCacheByURLsOperation{client}
}

// Definition returns the generated purgeCacheByURLs contract.
func (PurgeCacheByURLsOperation) Definition() sdkgo.MutationDefinition {
	return PurgeCacheByURLsDefinition
}

// IdempotencyKey identifies the Dex call; purging the same URLs is repeat safe.
func (PurgeCacheByURLsOperation) IdempotencyKey(callID sdkgo.CallID, _ PurgeCacheByURLsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates input, calls Cloudflare, and classifies the response.
func (operation PurgeCacheByURLsOperation) Invoke(call sdkgo.Call, input PurgeCacheByURLsInput) sdkgo.MutationAttempt[CachePurge] {
	spec, err := buildPurgeCacheByURLsRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(PurgeCacheByURLsBranchDefect, CachePurge{}, failure(sdkgo.FailureValidation, "purgeCacheByURLs", err.Error()), sdkgo.Receipt{})
	}
	return executeMutation(operation.client, call, "purgeCacheByURLs", spec, PurgeCacheByURLsBranchPurged, PurgeCacheByURLsBranchNotFound, PurgeCacheByURLsBranchProviderRejected, PurgeCacheByURLsBranchInvalidResponse, PurgeCacheByURLsBranchDefect, decodePurgeCacheByURLs)
}
func buildPurgeCacheByURLsRequest(input PurgeCacheByURLsInput) (requestSpec, error) {
	if strings.TrimSpace(input.ZoneID) == "" || len(input.URLs) == 0 || len(input.URLs) > 30 {
		return requestSpec{}, fmt.Errorf("zoneId and 1 to 30 URLs are required")
	}
	for _, rawURL := range input.URLs {
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return requestSpec{}, fmt.Errorf("each URL must be an absolute HTTP or HTTPS URL")
		}
	}
	body, err := json.Marshal(map[string]any{"files": input.URLs})
	if err != nil {
		return requestSpec{}, err
	}
	return requestSpec{method: http.MethodPost, path: "/zones/" + url.PathEscape(input.ZoneID) + "/purge_cache", body: body, contentType: "application/json"}, nil
}
func decodePurgeCacheByURLs(body []byte) (CachePurge, error) {
	var value CachePurge
	if err := json.Unmarshal(body, &value); err != nil {
		return CachePurge{}, err
	}
	if !value.Success {
		return CachePurge{}, fmt.Errorf("purge was not accepted")
	}
	return value, nil
}

func executeQuery[T any](client *Client, call sdkgo.Call, operation string, spec requestSpec, happy, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	response, localFailure, err := client.send(call, operation, spec)
	if localFailure != nil {
		localFailure.Operation = operation
		return sdkgo.NewQueryBranch(defect, zero, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "cloudflare", Operation: operation, Message: "Cloudflare request failed"}, 0)
	}
	// Closing the response cannot change its classified provider outcome.
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, operation)
		return sdkgo.NewQueryRetry[T](retry, after)
	}
	if response.StatusCode == http.StatusNotFound && notFound != "" {
		rejectedFailure := statusFailure(response, operation)
		return sdkgo.NewQueryBranch(notFound, zero, &rejectedFailure, sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusOK {
		rejectedFailure := statusFailure(response, operation)
		return sdkgo.NewQueryBranch(rejected, zero, &rejectedFailure, sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		if errors.Is(err, providerhttp.ErrBodyTooLarge) {
			return sdkgo.NewQueryBranch(invalid, zero, failure(sdkgo.FailureResponseTooLarge, operation, "response exceeded the configured limit"), sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "cloudflare", Operation: operation, Message: "response was interrupted"}, 0)
	}
	if isCloudflareRejection(body) {
		return sdkgo.NewQueryBranch(rejected, zero, failure(sdkgo.FailureProviderRejection, operation, "Cloudflare rejected the request"), sdkgo.Receipt{})
	}
	value, err := decode(body)
	if err != nil {
		return sdkgo.NewQueryBranch(invalid, zero, failure(sdkgo.FailureProtocol, operation, "response was malformed"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(happy, value, nil, sdkgo.Receipt{})
}

func executeMutation[T any](client *Client, call sdkgo.Call, operation string, spec requestSpec, happy, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	var zero T
	response, localFailure, err := client.send(call, operation, spec)
	if localFailure != nil {
		localFailure.Operation = operation
		return sdkgo.NewMutationBranch(defect, zero, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "cloudflare", Operation: operation, Message: "Cloudflare request failed"}, 0)
	}
	// Closing the response cannot change its classified provider outcome.
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, operation)
		return sdkgo.NewMutationRetry[T](retry, after)
	}
	if response.StatusCode == http.StatusNotFound && notFound != "" {
		rejectedFailure := statusFailure(response, operation)
		return sdkgo.NewMutationBranch(notFound, zero, &rejectedFailure, sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusOK {
		rejectedFailure := statusFailure(response, operation)
		return sdkgo.NewMutationBranch(rejected, zero, &rejectedFailure, sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		if errors.Is(err, providerhttp.ErrBodyTooLarge) {
			return sdkgo.NewMutationBranch(invalid, zero, failure(sdkgo.FailureResponseTooLarge, operation, "response exceeded the configured limit"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "cloudflare", Operation: operation, Message: "response was interrupted"}, 0)
	}
	if isCloudflareRejection(body) {
		return sdkgo.NewMutationBranch(rejected, zero, failure(sdkgo.FailureProviderRejection, operation, "Cloudflare rejected the request"), sdkgo.Receipt{})
	}
	value, err := decode(body)
	if err != nil {
		return sdkgo.NewMutationBranch(invalid, zero, failure(sdkgo.FailureProtocol, operation, "response was malformed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(happy, value, nil, sdkgo.Receipt{})
}

func (client *Client) send(call sdkgo.Call, operation string, spec requestSpec) (*http.Response, *sdkgo.Failure, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, failure(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable"), nil
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return nil, failure(sdkgo.FailureAuthentication, operation, "connection credentials are invalid"), nil
	}
	endpoint := client.baseURL + spec.path
	if len(spec.query) != 0 {
		endpoint += "?" + spec.query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, spec.method, endpoint, bytes.NewReader(spec.body))
	if err != nil {
		return nil, failure(sdkgo.FailureLocalDefect, operation, "request could not be built"), nil
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIToken.Reveal())
	request.Header.Set("Accept", "application/json")
	if spec.contentType != "" {
		request.Header.Set("Content-Type", spec.contentType)
	}
	response, err := client.httpClient.Do(request)
	return response, nil, err
}

func statusFailure(response *http.Response, operation string) sdkgo.Failure {
	kind := sdkgo.FailureProviderRejection
	switch response.StatusCode {
	case http.StatusUnauthorized:
		kind = sdkgo.FailureAuthentication
	case http.StatusForbidden:
		kind = sdkgo.FailureAuthorization
	case http.StatusNotFound:
		kind = sdkgo.FailureNotFound
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		kind = sdkgo.FailureValidation
	}
	return sdkgo.Failure{Kind: kind, Provider: "cloudflare", Operation: operation, Message: fmt.Sprintf("Cloudflare returned HTTP %d", response.StatusCode)}
}

func shouldRetry(response *http.Response) bool {
	return response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError
}

func retryFailure(response *http.Response, operation string) (sdkgo.Failure, time.Duration) {
	kind := sdkgo.FailureAvailability
	if response.StatusCode == http.StatusTooManyRequests {
		kind = sdkgo.FailureRateLimit
	}
	return sdkgo.Failure{Kind: kind, Provider: "cloudflare", Operation: operation, Message: fmt.Sprintf("Cloudflare returned HTTP %d", response.StatusCode)}, providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
}

func failure(kind sdkgo.FailureKind, operation, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "cloudflare", Operation: operation, Message: message}
}

func isCloudflareRejection(body []byte) bool {
	var envelope struct {
		Success *bool `json:"success"`
	}
	return json.Unmarshal(body, &envelope) == nil && envelope.Success != nil && !*envelope.Success
}
