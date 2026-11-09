// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workday

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var workerIDPattern = regexp.MustCompile(`^[A-Fa-f0-9]{32}$`)

// GetWorkerInput identifies a worker by its Workday ID.
type GetWorkerInput struct {
	// WorkerID is the WID returned by the Workday workers REST endpoint.
	WorkerID string `json:"workerId"`
}

// WorkerSummary contains the stable WID and displayed worker name.
type WorkerSummary struct {
	// ID is the worker's Workday ID.
	ID string `json:"id"`
	// Descriptor is the worker's display name.
	Descriptor string `json:"descriptor"`
}

// GetWorkerOperation reads a worker without mutating the provider.
type GetWorkerOperation struct{ client *Client }

// GetWorker binds the Query to this client.
func (client *Client) GetWorker() GetWorkerOperation { return GetWorkerOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetWorkerOperation) Definition() sdkgo.QueryDefinition { return GetWorkerDefinition }

// Invoke classifies token, transport, HTTP, and worker response outcomes.
func (operation GetWorkerOperation) Invoke(call sdkgo.Call, input GetWorkerInput) sdkgo.QueryAttempt[WorkerSummary] {
	if !workerIDPattern.MatchString(input.WorkerID) {
		return workerBranch(GetWorkerBranchDefect, sdkgo.FailureValidation, "worker WID is invalid")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return workerBranch(GetWorkerBranchDefect, sdkgo.FailureAuthentication, "Workday connection needs authorization")
		}
		return workerRetry(sdkgo.FailureTransport, "Workday credential refresh failed", 0)
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return workerBranch(GetWorkerBranchDefect, sdkgo.FailureAuthentication, "Workday access token is unavailable")
	}
	endpoint := operation.client.baseURL + "/workers/" + url.PathEscape(input.WorkerID)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return workerBranch(GetWorkerBranchDefect, sdkgo.FailureValidation, "worker request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return workerRetry(sdkgo.FailureTransport, "Workday worker request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return workerBranch(GetWorkerBranchDefect, sdkgo.FailureAuthentication, "Workday response contained a credential")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return workerRetry(sdkgo.FailureRateLimit, "Workday rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return workerRetry(sdkgo.FailureAvailability, "Workday is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return workerBranch(GetWorkerBranchNotFound, sdkgo.FailureNotFound, "Workday worker was not found")
	}
	if response.StatusCode != http.StatusOK {
		return workerBranch(GetWorkerBranchProviderRejected, sdkgo.FailureProviderRejection, "Workday rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return workerBranch(GetWorkerBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Workday worker response exceeded the configured limit")
	}
	if readErr != nil {
		return workerRetry(sdkgo.FailureTransport, "Workday worker response was interrupted", 0)
	}
	worker, err := decodeWorker(body, input.WorkerID)
	if err != nil {
		return workerBranch(GetWorkerBranchInvalidResponse, sdkgo.FailureProtocol, "Workday returned invalid worker JSON")
	}
	return sdkgo.NewQueryBranch(GetWorkerBranchFound, worker, nil, sdkgo.Receipt{})
}

func decodeWorker(body []byte, requestedID string) (WorkerSummary, error) {
	var worker WorkerSummary
	if err := json.Unmarshal(body, &worker); err != nil {
		return WorkerSummary{}, err
	}
	if worker.ID != requestedID || worker.Descriptor == "" {
		return WorkerSummary{}, errors.New("worker result is incomplete or has a different WID")
	}
	return worker, nil
}

func workerFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "workday", Operation: "getWorker", Message: message}
}

func workerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WorkerSummary] {
	failure := workerFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, WorkerSummary{}, &failure, sdkgo.Receipt{})
}

func workerRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[WorkerSummary] {
	return sdkgo.NewQueryRetry[WorkerSummary](workerFailure(kind, message), delay)
}
