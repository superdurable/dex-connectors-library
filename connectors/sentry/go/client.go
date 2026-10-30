// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sentry implements the Sentry JSON API as typed Dex connector Steps.
package sentry

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

// ListIssuesInput selects one organization's current issues.
type ListIssuesInput struct {
	// Organization is the Sentry organization slug.
	Organization string `json:"organization"`
	// Query is the provider search expression.
	Query string `json:"query"`
}

// GetIssueInput identifies one issue within an organization.
type GetIssueInput struct {
	// Organization is the Sentry organization slug.
	Organization string `json:"organization"`
	// ID is the provider's stable resource identifier.
	ID string `json:"id"`
}

// UpdateIssueStatusInput sets one issue's absolute status.
type UpdateIssueStatusInput struct {
	// Organization is the Sentry organization slug.
	Organization string `json:"organization"`
	// ID is the provider's stable resource identifier.
	ID string `json:"id"`
	// Status is the provider's current status.
	Status string `json:"status"`
}

// Issue contains selected issue metadata.
type Issue struct {
	// ID is the provider's stable resource identifier.
	ID string `json:"id"`
	// ShortID is the human-readable Sentry issue identifier.
	ShortID string `json:"shortId"`
	// Title is the provider's display title.
	Title string `json:"title"`
	// Status is the provider's current status.
	Status string `json:"status"`
}

// IssuePage is one organization issue response page.
type IssuePage struct {
	// Issues contains one Sentry issue response page.
	Issues []Issue `json:"issues"`
}

// ListIssuesOperation invokes Sentry's listIssues endpoint.
type ListIssuesOperation struct {
	client *Client
}

// Definition returns the generated listIssues contract.
func (ListIssuesOperation) Definition() sdkgo.QueryDefinition { return ListIssuesDefinition }

// GetIssueOperation invokes Sentry's getIssue endpoint.
type GetIssueOperation struct {
	client *Client
}

// Definition returns the generated getIssue contract.
func (GetIssueOperation) Definition() sdkgo.QueryDefinition { return GetIssueDefinition }

// UpdateIssueStatusOperation invokes Sentry's updateIssueStatus endpoint.
type UpdateIssueStatusOperation struct {
	client *Client
}

// Definition returns the generated updateIssueStatus contract.
func (UpdateIssueStatusOperation) Definition() sdkgo.MutationDefinition {
	return UpdateIssueStatusDefinition
}

// New validates configuration without a network call. Zero settings use manifest defaults.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Sentry credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Sentry base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListIssues binds listIssues to this client.
func (client *Client) ListIssues() ListIssuesOperation { return ListIssuesOperation{client} }

// GetIssue binds getIssue to this client.
func (client *Client) GetIssue() GetIssueOperation { return GetIssueOperation{client} }

// UpdateIssueStatus binds updateIssueStatus to this client.
func (client *Client) UpdateIssueStatus() UpdateIssueStatusOperation {
	return UpdateIssueStatusOperation{client}
}

// Invoke runs this provider operation.
func (operation ListIssuesOperation) Invoke(call sdkgo.Call, input ListIssuesInput) sdkgo.QueryAttempt[IssuePage] {
	if input.Organization == "" {
		return queryFailure[IssuePage]("listIssues", ListIssuesBranchDefect, sdkgo.FailureValidation, "organization slug is required")
	}
	path := "/organizations/" + url.PathEscape(input.Organization) + "/issues/"
	if input.Query != "" {
		path += "?query=" + url.QueryEscape(input.Query)
	}
	return runQuery(operation.client, call, "listIssues", path, ListIssuesBranchListed, "", ListIssuesBranchProviderRejected, ListIssuesBranchInvalidResponse, ListIssuesBranchDefect, func(body []byte) (IssuePage, error) {
		var issues []Issue
		err := json.Unmarshal(body, &issues)
		if err == nil && issues == nil {
			err = fmt.Errorf("issues missing")
		}
		return IssuePage{Issues: issues}, err
	})
}

// Invoke runs this provider operation.
func (operation GetIssueOperation) Invoke(call sdkgo.Call, input GetIssueInput) sdkgo.QueryAttempt[Issue] {
	if input.Organization == "" || input.ID == "" {
		return queryFailure[Issue]("getIssue", GetIssueBranchDefect, sdkgo.FailureValidation, "organization and issue ID are required")
	}
	return runQuery(operation.client, call, "getIssue", "/organizations/"+url.PathEscape(input.Organization)+"/issues/"+url.PathEscape(input.ID)+"/", GetIssueBranchFound, GetIssueBranchNotFound, GetIssueBranchProviderRejected, GetIssueBranchInvalidResponse, GetIssueBranchDefect, decodeIssue)
}

// IdempotencyKey derives a stable provider-safe call key.
func (operation UpdateIssueStatusOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateIssueStatusInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke runs this provider operation.
func (operation UpdateIssueStatusOperation) Invoke(call sdkgo.Call, input UpdateIssueStatusInput) sdkgo.MutationAttempt[Issue] {
	if input.Organization == "" || input.ID == "" {
		return mutationFailure[Issue]("updateIssueStatus", UpdateIssueStatusBranchDefect, sdkgo.FailureValidation, "organization and issue ID are required")
	}
	switch input.Status {
	case "resolved", "unresolved", "ignored":
	default:
		return mutationFailure[Issue]("updateIssueStatus", UpdateIssueStatusBranchDefect, sdkgo.FailureValidation, "status must be resolved, unresolved, or ignored")
	}
	body, err := json.Marshal(struct {
		Status string `json:"status"`
	}{input.Status})
	if err != nil {
		return mutationFailure[Issue]("updateIssueStatus", UpdateIssueStatusBranchDefect, sdkgo.FailureValidation, "status cannot be encoded")
	}
	return runMutation(operation.client, call, "updateIssueStatus", http.MethodPut, "/organizations/"+url.PathEscape(input.Organization)+"/issues/"+url.PathEscape(input.ID)+"/", body, "", "", UpdateIssueStatusBranchUpdated, UpdateIssueStatusBranchNotFound, UpdateIssueStatusBranchProviderRejected, UpdateIssueStatusBranchInvalidResponse, UpdateIssueStatusBranchDefect, decodeIssue)
}
func decodeIssue(body []byte) (Issue, error) {
	var issue Issue
	err := json.Unmarshal(body, &issue)
	if err == nil && issue.ID == "" {
		err = fmt.Errorf("issue ID missing")
	}
	return issue, err
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
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("User-Agent", "superdurable-sentry/0.1")
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
	return sdkgo.Failure{Kind: kind, Provider: "sentry", Operation: operation, Message: message}
}
