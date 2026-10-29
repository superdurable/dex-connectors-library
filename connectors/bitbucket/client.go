// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package bitbucket provides read-only Bitbucket Query operations for Dex Flows.
package bitbucket

import (
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

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and resolves credentials for each request.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates the connection settings without contacting Bitbucket.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Bitbucket credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListRepositoriesInput identifies the workspace whose repositories are read.
type ListRepositoriesInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
}

// Repository is a minimal Bitbucket repository summary.
type Repository struct {
	// UUID is the provider repository ID.
	UUID string `json:"uuid"`
	// Name is its display name.
	Name string `json:"name"`
	// FullName combines workspace and repository slug.
	FullName string `json:"full_name"`
	// IsPrivate reports repository visibility.
	IsPrivate bool `json:"is_private"`
}

// RepositoryPage is one Bitbucket repository page.
type RepositoryPage struct {
	// Values contains repositories, possibly none.
	Values []Repository `json:"values"`
	// Next is the provider's next page URL, if any; applications must treat it as untrusted.
	Next string `json:"next"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PageLength is the provider page length.
	PageLength int `json:"pagelen"`
}

// ListPullRequestsInput identifies a repository within a workspace.
type ListPullRequestsInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug within the workspace.
	Repository string `json:"repository"`
}

// PullRequest is a minimal Bitbucket pull request summary.
type PullRequest struct {
	// ID is the repository-local numeric pull request ID.
	ID int64 `json:"id"`
	// Title is the pull request title.
	Title string `json:"title"`
	// State is the provider state such as OPEN.
	State string `json:"state"`
}

// PullRequestPage is one Bitbucket pull request page.
type PullRequestPage struct {
	// Values contains pull requests, possibly none.
	Values []PullRequest `json:"values"`
	// Next is the provider's next page URL, if any; applications must treat it as untrusted.
	Next string `json:"next"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PageLength is the provider page length.
	PageLength int `json:"pagelen"`
}

// ListRepositoriesOperation executes the listRepositories read.
type ListRepositoriesOperation struct{ client *Client }

// ListRepositories returns the listRepositories Query bound to this client.
func (client *Client) ListRepositories() ListRepositoriesOperation {
	return ListRepositoriesOperation{client: client}
}

// Definition returns the manifest-generated listRepositories contract.
func (ListRepositoriesOperation) Definition() sdkgo.QueryDefinition {
	return ListRepositoriesDefinition
}

// Invoke reads the provider resource and classifies the response.
func (operation ListRepositoriesOperation) Invoke(call sdkgo.Call, input ListRepositoriesInput) sdkgo.QueryAttempt[RepositoryPage] {
	query := url.Values{}
	if !validPathSegment(input.Workspace) {
		return failedAttempt[RepositoryPage]("listRepositories", ListRepositoriesBranchDefect, sdkgo.FailureValidation, "workspace must be a path segment")
	}
	path := "/repositories/" + url.PathEscape(input.Workspace)
	return executeRead(operation.client, call, "listRepositories", path, query, ListRepositoriesBranchListed, ListRepositoriesBranchNotFound, ListRepositoriesBranchProviderRejected, ListRepositoriesBranchInvalidResponse, ListRepositoriesBranchDefect, decodeListRepositoriesResponse)
}

func decodeListRepositoriesResponse(body []byte) (RepositoryPage, error) {
	var response RepositoryPage
	err := json.Unmarshal(body, &response)
	if err != nil {
		return RepositoryPage{}, err
	}
	if response.Values == nil {
		return RepositoryPage{}, fmt.Errorf("missing values")
	}
	return response, nil
}

// ListPullRequestsOperation executes the listPullRequests read.
type ListPullRequestsOperation struct{ client *Client }

// ListPullRequests returns the listPullRequests Query bound to this client.
func (client *Client) ListPullRequests() ListPullRequestsOperation {
	return ListPullRequestsOperation{client: client}
}

// Definition returns the manifest-generated listPullRequests contract.
func (ListPullRequestsOperation) Definition() sdkgo.QueryDefinition {
	return ListPullRequestsDefinition
}

// Invoke reads the provider resource and classifies the response.
func (operation ListPullRequestsOperation) Invoke(call sdkgo.Call, input ListPullRequestsInput) sdkgo.QueryAttempt[PullRequestPage] {
	query := url.Values{}
	if !validPathSegment(input.Workspace) {
		return failedAttempt[PullRequestPage]("listPullRequests", ListPullRequestsBranchDefect, sdkgo.FailureValidation, "workspace must be a path segment")
	}
	path := "/repositories/" + url.PathEscape(input.Workspace)
	if !validPathSegment(input.Repository) {
		return failedAttempt[PullRequestPage]("listPullRequests", ListPullRequestsBranchDefect, sdkgo.FailureValidation, "repository must be a path segment")
	}
	path += "/" + url.PathEscape(input.Repository) + "/pullrequests"
	return executeRead(operation.client, call, "listPullRequests", path, query, ListPullRequestsBranchListed, ListPullRequestsBranchNotFound, ListPullRequestsBranchProviderRejected, ListPullRequestsBranchInvalidResponse, ListPullRequestsBranchDefect, decodeListPullRequestsResponse)
}

func decodeListPullRequestsResponse(body []byte) (PullRequestPage, error) {
	var response PullRequestPage
	err := json.Unmarshal(body, &response)
	if err != nil {
		return PullRequestPage{}, err
	}
	if response.Values == nil {
		return PullRequestPage{}, fmt.Errorf("missing values")
	}
	return response, nil
}

type authResult struct {
	failure    *sdkgo.Failure
	branch     sdkgo.BranchID
	retry      bool
	retryAfter time.Duration
}

func (client *Client) authorize(call sdkgo.Call, operation string, request *http.Request, defectBranch, rejectedBranch, invalidBranch sdkgo.BranchID) authResult {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return authResult{failure: failureFor(operation, sdkgo.FailureAuthentication, "Bitbucket credentials are unavailable"), branch: defectBranch}
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	return authResult{}
}

func executeRead[T any](client *Client, call sdkgo.Call, operation, path string, query url.Values, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	endpoint := client.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return failedAttempt[T](operation, defectBranch, sdkgo.FailureLocalDefect, "provider request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := client.authorize(call, operation, request, defectBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		if authorization.retry {
			return sdkgo.NewQueryRetry[T](*authorization.failure, authorization.retryAfter)
		}
		return sdkgo.NewQueryBranch(authorization.branch, *new(T), authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider read failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureRateLimit, "provider rate limited the read"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider read is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return failedAttempt[T](operation, notFoundBranch, sdkgo.FailureProviderRejection, "resource was not found")
	case response.StatusCode == http.StatusUnauthorized:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureAuthentication, "provider rejected credentials")
	case response.StatusCode == http.StatusForbidden:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureAuthorization, "provider denied access")
	case response.StatusCode != http.StatusOK:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureProviderRejection, "provider rejected read with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedAttempt[T](operation, invalidBranch, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(body)
	if err != nil {
		return failedAttempt[T](operation, invalidBranch, sdkgo.FailureProtocol, "provider returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(successBranch, value, nil, sdkgo.Receipt{})
}

func validPathSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/?#") && strings.IndexFunc(value, func(character rune) bool { return character < 32 }) < 0
}

func failedAttempt[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryBranch(branch, *new(T), failureFor(operation, kind, message), sdkgo.Receipt{})
}
func failureFor(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operation, Message: message}
}
