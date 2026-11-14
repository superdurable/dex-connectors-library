// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oceanbase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListInstancesInput selects a one-based page and number of instances per page.
type ListInstancesInput struct {
	// PageNumber defaults to 1 when omitted; valid values are positive.
	PageNumber int64 `json:"pageNumber,omitempty"`
	// PageSize defaults to 10 when omitted; valid values are 1 through 100.
	PageSize int64 `json:"pageSize,omitempty"`
}

// InstanceInfo contains selected non-secret cluster fields.
type InstanceInfo struct {
	// ID is the provider's cluster ID.
	ID string `json:"id"`
	// Name is the cluster display name.
	Name string `json:"name"`
	// State is the provider's cluster state.
	State string `json:"state"`
	// Region is the cluster deployment region.
	Region string `json:"region"`
}

// ListInstancesOutput contains one page and the provider's total count.
type ListInstancesOutput struct {
	// Items contains this page's instances.
	Items []InstanceInfo `json:"items"`
	// Total is the total number of accessible instances in the project.
	Total int64 `json:"total"`
	// PageNumber is the requested one-based page.
	PageNumber int64 `json:"pageNumber"`
	// PageSize is the requested maximum number of instances.
	PageSize int64 `json:"pageSize"`
}

// ListInstancesOperation reads one page of project instances.
type ListInstancesOperation struct{ client *Client }

// Definition returns the generated instance-list contract.
func (ListInstancesOperation) Definition() sdkgo.QueryDefinition { return ListInstancesDefinition }

// Invoke completes one Digest challenge and response without exposing AccessKey material.
func (operation ListInstancesOperation) Invoke(call sdkgo.Call, input ListInstancesInput) sdkgo.QueryAttempt[ListInstancesOutput] {
	if input.PageNumber == 0 {
		input.PageNumber = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 10
	}
	if input.PageNumber < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return instancesBranch(ListInstancesBranchDefect, sdkgo.FailureValidation, "pageNumber or pageSize is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AccessKeyID.Reveal()) || !isValidCredential(credentials.AccessKeySecret.Reveal()) || !isValidProjectID(credentials.ProjectID.Reveal()) {
		return instancesBranch(ListInstancesBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	requestID, err := newRequestID()
	if err != nil {
		return instancesBranch(ListInstancesBranchDefect, sdkgo.FailureAvailability, "request ID could not be generated")
	}
	endpoint, err := url.Parse(operation.client.baseURL + "/api/v2/instances")
	if err != nil {
		return instancesBranch(ListInstancesBranchDefect, sdkgo.FailureValidation, "instance endpoint is invalid")
	}
	query := endpoint.Query()
	query.Set("requestId", requestID)
	query.Set("pageNumber", strconv.FormatInt(input.PageNumber, 10))
	query.Set("pageSize", strconv.FormatInt(input.PageSize, 10))
	endpoint.RawQuery = query.Encode()
	response, err := operation.client.sendInstancesRequest(call.Context, endpoint.String(), credentials.ProjectID.Reveal(), "")
	if err != nil {
		return instancesRetry(sdkgo.FailureAvailability, "OceanBase Cloud request failed", 0)
	}
	if response.StatusCode == http.StatusUnauthorized {
		challenge, challengeErr := parseDigestChallenge(response.Header.Get("WWW-Authenticate"))
		_ = response.Body.Close()
		if challengeErr != nil {
			return instancesBranch(ListInstancesBranchProviderRejected, sdkgo.FailureAuthentication, "OceanBase Cloud returned an unsupported Digest challenge")
		}
		authorization, authErr := digestAuthorization(challenge, credentials.AccessKeyID.Reveal(), credentials.AccessKeySecret.Reveal(), http.MethodGet, endpoint.RequestURI())
		if authErr != nil {
			return instancesBranch(ListInstancesBranchDefect, sdkgo.FailureAvailability, "Digest authorization could not be constructed")
		}
		response, err = operation.client.sendInstancesRequest(call.Context, endpoint.String(), credentials.ProjectID.Reveal(), authorization)
		if err != nil {
			return instancesRetry(sdkgo.FailureAvailability, "OceanBase Cloud request failed", 0)
		}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return instancesRetry(sdkgo.FailureRateLimit, "OceanBase Cloud rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return instancesRetry(sdkgo.FailureAvailability, "OceanBase Cloud is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return instancesBranch(ListInstancesBranchProviderRejected, kind, "OceanBase Cloud rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return instancesBranch(ListInstancesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "OceanBase Cloud response exceeded the configured limit")
	}
	if err != nil {
		return instancesRetry(sdkgo.FailureAvailability, "OceanBase Cloud response was interrupted", 0)
	}
	var status struct {
		Success *bool `json:"success"`
	}
	if json.Unmarshal(body, &status) != nil || status.Success == nil {
		return instancesBranch(ListInstancesBranchInvalidResponse, sdkgo.FailureProtocol, "OceanBase Cloud returned invalid instance response")
	}
	if !*status.Success {
		return instancesBranch(ListInstancesBranchProviderRejected, sdkgo.FailureProviderRejection, "OceanBase Cloud rejected the instance query")
	}
	output, err := decodeInstances(body, input, credentials)
	if err != nil {
		return instancesBranch(ListInstancesBranchInvalidResponse, sdkgo.FailureProtocol, "OceanBase Cloud returned invalid instance data")
	}
	return sdkgo.NewQueryBranch(ListInstancesBranchListed, output, nil, sdkgo.Receipt{})
}
func (client *Client) sendInstancesRequest(ctx context.Context, endpoint string, projectID string, authorization string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Ob-Project-Id", projectID)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	return client.httpClient.Do(request)
}
func decodeInstances(body []byte, input ListInstancesInput, credentials Credentials) (ListInstancesOutput, error) {
	var wire struct {
		Success *bool `json:"success"`
		Data    struct {
			DataList []struct {
				ID     string `json:"instanceId"`
				Name   string `json:"instanceName"`
				State  string `json:"state"`
				Region string `json:"region"`
			} `json:"dataList"`
		} `json:"data"`
		TotalCount *int64 `json:"totalCount"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Success == nil {
		return ListInstancesOutput{}, errors.New("invalid instance response")
	}
	if !*wire.Success {
		return ListInstancesOutput{}, errors.New("provider rejected instance query")
	}
	if wire.Data.DataList == nil || wire.TotalCount == nil || *wire.TotalCount < 0 || int64(len(wire.Data.DataList)) > input.PageSize {
		return ListInstancesOutput{}, errors.New("invalid instance page")
	}
	output := ListInstancesOutput{Items: make([]InstanceInfo, 0, len(wire.Data.DataList)), Total: *wire.TotalCount, PageNumber: input.PageNumber, PageSize: input.PageSize}
	for _, item := range wire.Data.DataList {
		if item.ID == "" || item.Name == "" || item.State == "" || item.Region == "" || len(item.ID) > 256 || len(item.Name) > 256 || len(item.State) > 64 || len(item.Region) > 128 || containsCredential(item.ID, credentials) || containsCredential(item.Name, credentials) || containsCredential(item.State, credentials) || containsCredential(item.Region, credentials) {
			return ListInstancesOutput{}, errors.New("invalid instance item")
		}
		output.Items = append(output.Items, InstanceInfo{ID: item.ID, Name: item.Name, State: item.State, Region: item.Region})
	}
	return output, nil
}
func containsCredential(value string, credentials Credentials) bool {
	return strings.Contains(value, credentials.AccessKeyID.Reveal()) || strings.Contains(value, credentials.AccessKeySecret.Reveal()) || strings.Contains(value, credentials.ProjectID.Reveal())
}
func newRequestID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
func instancesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListInstancesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "oceanbase", Operation: "listInstances", Message: message}
	return sdkgo.NewQueryBranch(branch, ListInstancesOutput{Items: []InstanceInfo{}}, &failure, sdkgo.Receipt{})
}
func instancesRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListInstancesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "oceanbase", Operation: "listInstances", Message: message}
	return sdkgo.NewQueryRetry[ListInstancesOutput](failure, delay)
}
