// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pingcap

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ListProjectsInput selects a one-based page and number of projects per page.
type ListProjectsInput struct {
	// Page defaults to 1 when omitted; valid values are positive.
	Page int64 `json:"page,omitempty"`
	// PageSize defaults to 10 when omitted; valid values are 1 through 100.
	PageSize int64 `json:"pageSize,omitempty"`
}

// ProjectInfo is one TiDB Cloud project accessible to the API key.
type ProjectInfo struct {
	// ID is the stable project identifier.
	ID string `json:"id"`
	// OrgID is the owning organization identifier.
	OrgID string `json:"orgId"`
	// Name is the project display name.
	Name string `json:"name"`
	// ClusterCount is the number of clusters in the project.
	ClusterCount int64 `json:"clusterCount"`
	// UserCount is the number of project users.
	UserCount int64 `json:"userCount"`
	// CreateTimestamp is the provider timestamp string.
	CreateTimestamp string `json:"createTimestamp"`
	// Type is the provider project type when supplied.
	Type string `json:"type,omitempty"`
}

// ListProjectsOutput contains one page and the provider's total count.
type ListProjectsOutput struct {
	// Items contains this page's projects.
	Items []ProjectInfo `json:"items"`
	// Total is the total number of accessible projects.
	Total int64 `json:"total"`
	// Page is the requested one-based page.
	Page int64 `json:"page"`
	// PageSize is the requested maximum number of projects.
	PageSize int64 `json:"pageSize"`
}

// ListProjectsOperation reads one page of projects.
type ListProjectsOperation struct{ client *Client }

// Definition returns the generated project-list contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Invoke completes one Digest challenge and response without exposing the private key.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ListProjectsOutput] {
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 10
	}
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return projectsBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "page or pageSize is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.PublicKey.Reveal()) || !isValidCredential(credentials.PrivateKey.Reveal()) {
		return projectsBranch(ListProjectsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	endpoint, err := url.Parse(operation.client.baseURL + "/api/v1beta/projects")
	if err != nil {
		return projectsBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "projects endpoint is invalid")
	}
	query := endpoint.Query()
	query.Set("page", strconv.FormatInt(input.Page, 10))
	query.Set("page_size", strconv.FormatInt(input.PageSize, 10))
	endpoint.RawQuery = query.Encode()
	response, err := operation.client.sendProjectsRequest(call.Context, endpoint.String(), "")
	if err != nil {
		return projectsRetry(sdkgo.FailureAvailability, "TiDB Cloud request failed", 0)
	}
	if response.StatusCode == http.StatusUnauthorized {
		challenge, challengeErr := parseDigestChallenge(response.Header.Get("WWW-Authenticate"))
		_ = response.Body.Close()
		if challengeErr != nil {
			return projectsBranch(ListProjectsBranchProviderRejected, sdkgo.FailureAuthentication, "TiDB Cloud returned an unsupported Digest challenge")
		}
		authorization, authErr := digestAuthorization(challenge, credentials.PublicKey.Reveal(), credentials.PrivateKey.Reveal(), http.MethodGet, endpoint.RequestURI())
		if authErr != nil {
			return projectsBranch(ListProjectsBranchDefect, sdkgo.FailureAvailability, "Digest authorization could not be constructed")
		}
		response, err = operation.client.sendProjectsRequest(call.Context, endpoint.String(), authorization)
		if err != nil {
			return projectsRetry(sdkgo.FailureAvailability, "TiDB Cloud request failed", 0)
		}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return projectsRetry(sdkgo.FailureRateLimit, "TiDB Cloud rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return projectsRetry(sdkgo.FailureAvailability, "TiDB Cloud is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return projectsBranch(ListProjectsBranchProviderRejected, kind, "TiDB Cloud rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return projectsBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "TiDB Cloud response exceeded the configured limit")
	}
	if err != nil {
		return projectsRetry(sdkgo.FailureAvailability, "TiDB Cloud response was interrupted", 0)
	}
	output, err := decodeProjects(body, input)
	if err != nil {
		return projectsBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "TiDB Cloud returned invalid project data")
	}
	return sdkgo.NewQueryBranch(ListProjectsBranchListed, output, nil, sdkgo.Receipt{})
}
func (client *Client) sendProjectsRequest(ctx context.Context, endpoint string, authorization string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	return client.httpClient.Do(request)
}
func decodeProjects(body []byte, input ListProjectsInput) (ListProjectsOutput, error) {
	var wire struct {
		Items []struct {
			ID              string `json:"id"`
			OrgID           string `json:"org_id"`
			Name            string `json:"name"`
			ClusterCount    int64  `json:"cluster_count"`
			UserCount       int64  `json:"user_count"`
			CreateTimestamp string `json:"create_timestamp"`
			Type            string `json:"type"`
		} `json:"items"`
		Total *int64 `json:"total"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Items == nil || wire.Total == nil || *wire.Total < 0 || int64(len(wire.Items)) > input.PageSize {
		return ListProjectsOutput{}, errors.New("invalid project page")
	}
	output := ListProjectsOutput{Items: make([]ProjectInfo, 0, len(wire.Items)), Total: *wire.Total, Page: input.Page, PageSize: input.PageSize}
	for _, item := range wire.Items {
		if item.ID == "" || item.OrgID == "" || item.Name == "" || item.ClusterCount < 0 || item.UserCount < 0 {
			return ListProjectsOutput{}, errors.New("invalid project item")
		}
		output.Items = append(output.Items, ProjectInfo{ID: item.ID, OrgID: item.OrgID, Name: item.Name, ClusterCount: item.ClusterCount, UserCount: item.UserCount, CreateTimestamp: item.CreateTimestamp, Type: item.Type})
	}
	return output, nil
}
func projectsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pingcap", Operation: "listProjects", Message: message}
	return sdkgo.NewQueryBranch(branch, ListProjectsOutput{Items: []ProjectInfo{}}, &failure, sdkgo.Receipt{})
}
func projectsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pingcap", Operation: "listProjects", Message: message}
	return sdkgo.NewQueryRetry[ListProjectsOutput](failure, delay)
}
