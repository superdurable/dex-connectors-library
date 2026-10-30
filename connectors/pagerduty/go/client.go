// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pagerduty implements the PagerDuty JSON API as typed Dex connector Steps.
package pagerduty

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

// Option supplies a client dependency during construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP settings and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// ListIncidentsInput chooses one incident page.
type ListIncidentsInput struct {
	// Limit is the maximum number of incidents requested.
	Limit int `json:"limit"`
	// Offset is the zero-based page offset.
	Offset int `json:"offset"`
}

// GetIncidentInput identifies an incident.
type GetIncidentInput struct {
	// ID is the provider's stable resource identifier.
	ID string `json:"id"`
}

// Incident contains selected incident fields.
type Incident struct {
	// ID is the provider's stable resource identifier.
	ID string `json:"id"`
	// Title is the provider's display title.
	Title string `json:"title"`
	// Status is the provider's current status.
	Status string `json:"status"`
}

// IncidentPage is one page of incidents.
type IncidentPage struct {
	// Incidents contains one incident page.
	Incidents []Incident `json:"incidents"`
	// Limit is the maximum number of incidents requested.
	Limit int `json:"limit"`
	// Offset is the zero-based page offset.
	Offset int `json:"offset"`
	// More reports whether another incident page exists.
	More bool `json:"more"`
}

// ListIncidentsOperation invokes PagerDuty's listIncidents endpoint.
type ListIncidentsOperation struct {
	client *Client
}

// Definition returns the generated listIncidents contract.
func (ListIncidentsOperation) Definition() sdkgo.QueryDefinition { return ListIncidentsDefinition }

// GetIncidentOperation invokes PagerDuty's getIncident endpoint.
type GetIncidentOperation struct {
	client *Client
}

// Definition returns the generated getIncident contract.
func (GetIncidentOperation) Definition() sdkgo.QueryDefinition { return GetIncidentDefinition }

// New validates configuration without a network call. Zero settings use manifest defaults.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("PagerDuty credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("PagerDuty base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListIncidents binds listIncidents to this client.
func (client *Client) ListIncidents() ListIncidentsOperation { return ListIncidentsOperation{client} }

// GetIncident binds getIncident to this client.
func (client *Client) GetIncident() GetIncidentOperation { return GetIncidentOperation{client} }

// Invoke runs this provider operation.
func (operation ListIncidentsOperation) Invoke(call sdkgo.Call, input ListIncidentsInput) sdkgo.QueryAttempt[IncidentPage] {
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return queryFailure[IncidentPage]("listIncidents", ListIncidentsBranchDefect, sdkgo.FailureValidation, "limit must be 1–100 and offset nonnegative")
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return runQuery(operation.client, call, "listIncidents", "/incidents?"+query.Encode(), ListIncidentsBranchListed, "", ListIncidentsBranchProviderRejected, ListIncidentsBranchInvalidResponse, ListIncidentsBranchDefect, func(body []byte) (IncidentPage, error) {
		var page IncidentPage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Incidents == nil {
			err = fmt.Errorf("incidents missing")
		}
		return page, err
	})
}

// Invoke runs this provider operation.
func (operation GetIncidentOperation) Invoke(call sdkgo.Call, input GetIncidentInput) sdkgo.QueryAttempt[Incident] {
	if input.ID == "" {
		return queryFailure[Incident]("getIncident", GetIncidentBranchDefect, sdkgo.FailureValidation, "incident ID is required")
	}
	return runQuery(operation.client, call, "getIncident", "/incidents/"+url.PathEscape(input.ID), GetIncidentBranchFound, GetIncidentBranchNotFound, GetIncidentBranchProviderRejected, GetIncidentBranchInvalidResponse, GetIncidentBranchDefect, func(body []byte) (Incident, error) {
		var envelope struct {
			Incident Incident `json:"incident"`
		}
		err := json.Unmarshal(body, &envelope)
		if err == nil && envelope.Incident.ID == "" {
			err = fmt.Errorf("incident missing")
		}
		return envelope.Incident, err
	})
}

func (client *Client) newRequest(call sdkgo.Call, method, path string, body []byte) (*http.Request, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, fmt.Errorf("credentials unavailable")
	}
	if err := credentials.Validate(); err != nil || strings.TrimSpace(credentials.APIKey.Reveal()) == "" || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, fmt.Errorf("credentials invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request invalid")
	}
	request.Header.Set("Authorization", "Token token="+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("User-Agent", "superdurable-pagerduty/0.1")
	return request, nil
}

func runQuery[T any](client *Client, call sdkgo.Call, operation, path string, success, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	request, err := client.newRequest(call, http.MethodGet, path, nil)
	if err != nil {
		return queryFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	// A read-only response close cannot affect the provider outcome.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))

	case response.StatusCode == http.StatusNotFound && notFound != "":
		return queryFailure[T](operation, notFound, sdkgo.FailureNotFound, "provider resource was not found")
	case response.StatusCode != http.StatusOK:
		return queryFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return queryFailure[T](operation, invalid, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(content)
	if err != nil {
		return queryFailure[T](operation, invalid, sdkgo.FailureProtocol, "provider response is incomplete or malformed")
	}
	return sdkgo.NewQueryBranch(success, value, nil, sdkgo.Receipt{})
}

func runMutation[T any](client *Client, call sdkgo.Call, operation, method, path string, body []byte, extraHeader, extraValue string, success, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	request, err := client.newRequest(call, method, path, body)
	if err != nil {
		return mutationFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	if extraHeader != "" {
		request.Header.Set(extraHeader, extraValue)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	// Closing the response cannot change a completed provider write.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusConflict:
		conflict, _ := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
		names := providerhttp.ReadErrorTokens(conflict, []string{"/name"})
		if len(names) == 1 && names[0] == "invalid_idempotent_request" {
			return mutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "idempotency key was used for a different request")
		}
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is processing the same request"), 0)
	case response.StatusCode == http.StatusNotFound && notFound != "":
		return mutationFailure[T](operation, notFound, sdkgo.FailureNotFound, "provider resource was not found")
	case response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusAccepted:
		return mutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return mutationFailure[T](operation, invalid, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(content)
	if err != nil {
		return mutationFailure[T](operation, invalid, sdkgo.FailureProtocol, "provider response is incomplete or malformed")
	}
	return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
}

func queryFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := newFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func mutationFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := newFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func newFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "pagerduty", Operation: operation, Message: message}
}
