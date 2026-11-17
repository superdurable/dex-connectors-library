// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workday

import (
	"bytes"
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

// ListWorkersOperation reads one page of accessible workers.
type ListWorkersOperation struct{ client *Client }

// ListWorkers binds the Query to this client.
func (client *Client) ListWorkers() ListWorkersOperation { return ListWorkersOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListWorkersOperation) Definition() sdkgo.QueryDefinition { return ListWorkersDefinition }

// Invoke classifies token, transport, HTTP, and page response outcomes.
func (operation ListWorkersOperation) Invoke(call sdkgo.Call, input ListWorkersInput) sdkgo.QueryAttempt[WorkerPage] {
	if input.Limit < 0 || input.Limit > 100 || input.Offset < 0 || (input.Search != "" && len(strings.TrimSpace(input.Search)) < 3) {
		return workerPageBranch(ListWorkersBranchDefect, sdkgo.FailureValidation, "worker page options are invalid")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return workerPageBranch(ListWorkersBranchDefect, sdkgo.FailureAuthentication, "Workday connection needs authorization")
		}
		return workerPageRetry(sdkgo.FailureTransport, "Workday credential refresh failed", 0)
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return workerPageBranch(ListWorkersBranchDefect, sdkgo.FailureAuthentication, "Workday access token is unavailable")
	}
	endpoint, err := url.Parse(operation.client.baseURL + "/workers")
	if err != nil {
		return workerPageBranch(ListWorkersBranchDefect, sdkgo.FailureValidation, "worker page request could not be built")
	}
	parameters := endpoint.Query()
	if input.Limit > 0 {
		parameters.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Offset > 0 {
		parameters.Set("offset", strconv.Itoa(input.Offset))
	}
	if input.Search != "" {
		parameters.Set("search", input.Search)
	}
	endpoint.RawQuery = parameters.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return workerPageBranch(ListWorkersBranchDefect, sdkgo.FailureValidation, "worker page request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return workerPageRetry(sdkgo.FailureTransport, "Workday worker page request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return workerPageBranch(ListWorkersBranchDefect, sdkgo.FailureAuthentication, "Workday response contained a credential")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return workerPageRetry(sdkgo.FailureRateLimit, "Workday rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return workerPageRetry(sdkgo.FailureAvailability, "Workday is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return workerPageBranch(ListWorkersBranchProviderRejected, sdkgo.FailureProviderRejection, "Workday rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return workerPageBranch(ListWorkersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Workday worker page exceeded the configured limit")
	}
	if readErr != nil {
		return workerPageRetry(sdkgo.FailureTransport, "Workday worker page response was interrupted", 0)
	}
	var page WorkerPage
	if err := json.Unmarshal(body, &page); err != nil || page.Total < 0 || page.Data == nil {
		return workerPageBranch(ListWorkersBranchInvalidResponse, sdkgo.FailureProtocol, "Workday returned invalid worker page JSON")
	}
	for _, worker := range page.Data {
		if !workerIDPattern.MatchString(worker.ID) || worker.Descriptor == "" {
			return workerPageBranch(ListWorkersBranchInvalidResponse, sdkgo.FailureProtocol, "Workday returned an incomplete worker page")
		}
	}
	return sdkgo.NewQueryBranch(ListWorkersBranchFound, page, nil, sdkgo.Receipt{})
}

func workerPageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "workday", Operation: "listWorkers", Message: message}
}

func workerPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WorkerPage] {
	failure := workerPageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, WorkerPage{}, &failure, sdkgo.Receipt{})
}

func workerPageRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[WorkerPage] {
	return sdkgo.NewQueryRetry[WorkerPage](workerPageFailure(kind, message), delay)
}
