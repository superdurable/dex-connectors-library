// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package gitlab exposes typed Dex Steps for the GitLab REST API.
package gitlab

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

// New validates configuration without contacting GitLab.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("GitLab credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("GitLab base URL: %w", err)
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

// GetProjectInput identifies a project by numeric ID or namespace path.
type GetProjectInput struct {
	// ProjectID is a numeric ID or namespace path, such as meridian/web.
	ProjectID string `json:"projectId"`
}

// Project is a GitLab project summary.
type Project struct {
	// ID is the project's numeric identifier.
	ID int64 `json:"id"`
	// Name is the project display name.
	Name string `json:"name"`
	// PathWithNamespace is the full group and project path.
	PathWithNamespace string `json:"path_with_namespace"`
	// WebURL links to the project's GitLab page.
	WebURL string `json:"web_url"`
}

// ListIssuesInput identifies the project whose issues are listed.
type ListIssuesInput struct {
	// ProjectID is a numeric ID or namespace path.
	ProjectID string `json:"projectId"`
}

// Issue is a GitLab issue summary.
type Issue struct {
	// ID is the global issue identifier.
	ID int64 `json:"id"`
	// IID is the issue number inside its project.
	IID int64 `json:"iid"`
	// Title is the issue title.
	Title string `json:"title"`
	// Labels is the issue's complete label set.
	Labels []string `json:"labels"`
	// WebURL links to the issue in GitLab.
	WebURL string `json:"web_url"`
}

// IssuesPage contains GitLab's default issue page.
type IssuesPage struct {
	// Issues contains one provider page, possibly empty.
	Issues []Issue `json:"issues"`
}

// UpdateIssueLabelsInput replaces every label on one issue; an empty list clears them.
type UpdateIssueLabelsInput struct {
	// ProjectID is a numeric ID or namespace path.
	ProjectID string `json:"projectId"`
	// IssueIID is the positive project-local issue number.
	IssueIID int64 `json:"issueIid"`
	// Labels is the desired complete label set; empty clears all labels.
	Labels []string `json:"labels"`
}

// GetProjectOperation invokes getProject with one provider request.
type GetProjectOperation struct{ client *Client }

// GetProject binds the operation to this client.
func (client *Client) GetProject() GetProjectOperation { return GetProjectOperation{client} }

// Definition returns the generated getProject contract.
func (GetProjectOperation) Definition() sdkgo.QueryDefinition { return GetProjectDefinition }

// Invoke validates input, calls GitLab, and classifies the response.
func (operation GetProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.QueryAttempt[Project] {
	spec, err := buildGetProjectRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetProjectBranchDefect, Project{}, failure(sdkgo.FailureValidation, "getProject", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "getProject", spec, GetProjectBranchFound, GetProjectBranchNotFound, GetProjectBranchProviderRejected, GetProjectBranchInvalidResponse, GetProjectBranchDefect, decodeGetProject)
}
func buildGetProjectRequest(input GetProjectInput) (requestSpec, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return requestSpec{}, fmt.Errorf("projectId is required")
	}
	return requestSpec{method: http.MethodGet, path: "/projects/" + url.PathEscape(input.ProjectID)}, nil
}
func decodeGetProject(body []byte) (Project, error) {
	var value Project
	if err := json.Unmarshal(body, &value); err != nil {
		return Project{}, err
	}
	if value.ID <= 0 {
		return Project{}, fmt.Errorf("missing project ID")
	}
	return value, nil
}

// ListIssuesOperation invokes listIssues with one provider request.
type ListIssuesOperation struct{ client *Client }

// ListIssues binds the operation to this client.
func (client *Client) ListIssues() ListIssuesOperation { return ListIssuesOperation{client} }

// Definition returns the generated listIssues contract.
func (ListIssuesOperation) Definition() sdkgo.QueryDefinition { return ListIssuesDefinition }

// Invoke validates input, calls GitLab, and classifies the response.
func (operation ListIssuesOperation) Invoke(call sdkgo.Call, input ListIssuesInput) sdkgo.QueryAttempt[IssuesPage] {
	spec, err := buildListIssuesRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListIssuesBranchDefect, IssuesPage{}, failure(sdkgo.FailureValidation, "listIssues", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "listIssues", spec, ListIssuesBranchListed, ListIssuesBranchNotFound, ListIssuesBranchProviderRejected, ListIssuesBranchInvalidResponse, ListIssuesBranchDefect, decodeListIssues)
}
func buildListIssuesRequest(input ListIssuesInput) (requestSpec, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return requestSpec{}, fmt.Errorf("projectId is required")
	}
	return requestSpec{method: http.MethodGet, path: "/projects/" + url.PathEscape(input.ProjectID) + "/issues"}, nil
}
func decodeListIssues(body []byte) (IssuesPage, error) {
	var items []Issue
	if err := json.Unmarshal(body, &items); err != nil {
		return IssuesPage{}, err
	}
	if items == nil {
		return IssuesPage{}, fmt.Errorf("missing issues")
	}
	return IssuesPage{Issues: items}, nil
}

// UpdateIssueLabelsOperation invokes updateIssueLabels with one provider request.
type UpdateIssueLabelsOperation struct{ client *Client }

// UpdateIssueLabels binds the operation to this client.
func (client *Client) UpdateIssueLabels() UpdateIssueLabelsOperation {
	return UpdateIssueLabelsOperation{client}
}

// Definition returns the generated updateIssueLabels contract.
func (UpdateIssueLabelsOperation) Definition() sdkgo.MutationDefinition {
	return UpdateIssueLabelsDefinition
}

// IdempotencyKey uses the stable call ID; this operation sets an absolute state.
func (UpdateIssueLabelsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateIssueLabelsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates input, calls GitLab, and classifies the response.
func (operation UpdateIssueLabelsOperation) Invoke(call sdkgo.Call, input UpdateIssueLabelsInput) sdkgo.MutationAttempt[Issue] {
	spec, err := buildUpdateIssueLabelsRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateIssueLabelsBranchDefect, Issue{}, failure(sdkgo.FailureValidation, "updateIssueLabels", err.Error()), sdkgo.Receipt{})
	}
	return executeMutation(operation.client, call, "updateIssueLabels", spec, UpdateIssueLabelsBranchUpdated, UpdateIssueLabelsBranchNotFound, UpdateIssueLabelsBranchProviderRejected, UpdateIssueLabelsBranchInvalidResponse, UpdateIssueLabelsBranchDefect, decodeUpdateIssueLabels)
}
func buildUpdateIssueLabelsRequest(input UpdateIssueLabelsInput) (requestSpec, error) {
	if strings.TrimSpace(input.ProjectID) == "" || input.IssueIID <= 0 {
		return requestSpec{}, fmt.Errorf("projectId and positive issueIid are required")
	}
	for _, label := range input.Labels {
		if strings.TrimSpace(label) == "" || strings.Contains(label, ",") {
			return requestSpec{}, fmt.Errorf("labels must be nonblank and cannot contain commas")
		}
	}
	form := url.Values{"labels": {strings.Join(input.Labels, ",")}}
	return requestSpec{method: http.MethodPut, path: "/projects/" + url.PathEscape(input.ProjectID) + "/issues/" + strconv.FormatInt(input.IssueIID, 10), body: []byte(form.Encode()), contentType: "application/x-www-form-urlencoded"}, nil
}
func decodeUpdateIssueLabels(body []byte) (Issue, error) {
	var value Issue
	if err := json.Unmarshal(body, &value); err != nil {
		return Issue{}, err
	}
	if value.ID <= 0 || value.IID <= 0 {
		return Issue{}, fmt.Errorf("missing issue identity")
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
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: operation, Message: "GitLab request failed"}, 0)
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
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: operation, Message: "response was interrupted"}, 0)
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
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: operation, Message: "GitLab request failed"}, 0)
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
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: operation, Message: "response was interrupted"}, 0)
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
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
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
	request.Header.Set("PRIVATE-TOKEN", credentials.AccessToken.Reveal())
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
	return sdkgo.Failure{Kind: kind, Provider: "gitlab", Operation: operation, Message: fmt.Sprintf("GitLab returned HTTP %d", response.StatusCode)}
}

func shouldRetry(response *http.Response) bool {
	return response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError
}

func retryFailure(response *http.Response, operation string) (sdkgo.Failure, time.Duration) {
	kind := sdkgo.FailureAvailability
	if response.StatusCode == http.StatusTooManyRequests {
		kind = sdkgo.FailureRateLimit
	}
	return sdkgo.Failure{Kind: kind, Provider: "gitlab", Operation: operation, Message: fmt.Sprintf("GitLab returned HTTP %d", response.StatusCode)}, providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
}

func failure(kind sdkgo.FailureKind, operation, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "gitlab", Operation: operation, Message: message}
}
