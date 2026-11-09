// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rippling

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetWorkerInput selects a worker by its non-secret Rippling ID.
type GetWorkerInput struct {
	// WorkerID is the ID shown in Rippling Workers or returned by the API.
	WorkerID string `json:"workerId"`
}

// WorkerSummary excludes personal and compensation details.
type WorkerSummary struct {
	// ID is the Rippling worker ID.
	ID string `json:"id"`
	// Title is the visible worker job title, or empty when unavailable.
	Title string `json:"title"`
	// Status is the visible employment state, or empty when unavailable.
	Status string `json:"status"`
}

// GetWorkerOperation reads one worker's identity and state.
type GetWorkerOperation struct{ client *Client }

// GetWorker binds the Query to this client.
func (client *Client) GetWorker() GetWorkerOperation {
	return GetWorkerOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (GetWorkerOperation) Definition() sdkgo.QueryDefinition { return GetWorkerDefinition }

// Invoke reads one worker and classifies retryable and definitive outcomes.
func (operation GetWorkerOperation) Invoke(call sdkgo.Call, input GetWorkerInput) sdkgo.QueryAttempt[WorkerSummary] {
	if !validWorkerID(input.WorkerID) {
		return workerBranch(GetWorkerBranchDefect, sdkgo.FailureValidation, "worker ID must contain 1–64 URL-safe characters")
	}
	response := operation.client.get(call, "/workers/"+url.PathEscape(input.WorkerID)+"/")
	if errors.Is(response.err, errCredential) {
		return workerBranch(GetWorkerBranchDefect, sdkgo.FailureAuthentication, "Rippling credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[WorkerSummary](workerFailure(sdkgo.FailureTransport, "Rippling request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[WorkerSummary](workerFailure(sdkgo.FailureRateLimit, "Rippling rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[WorkerSummary](workerFailure(sdkgo.FailureAvailability, "Rippling is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return workerBranch(GetWorkerBranchNotFound, sdkgo.FailureNotFound, "Rippling did not expose the worker")
	}
	if response.status != http.StatusOK {
		return workerBranch(GetWorkerBranchProviderRejected, sdkgo.FailureProviderRejection, "Rippling rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return workerBranch(GetWorkerBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Rippling response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[WorkerSummary](workerFailure(sdkgo.FailureTransport, "Rippling response was interrupted"), 0)
	}
	workerSummary, err := decodeWorker(response.body, input.WorkerID)
	if err != nil {
		return workerBranch(GetWorkerBranchInvalidResponse, sdkgo.FailureProtocol, "Rippling returned invalid worker JSON")
	}
	return sdkgo.NewQueryBranch(GetWorkerBranchFound, workerSummary, nil, sdkgo.Receipt{})
}

func validWorkerID(workerID string) bool {
	if len(workerID) == 0 || len(workerID) > 64 {
		return false
	}
	for _, character := range workerID {
		if character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodeWorker(body []byte, requestedWorkerID string) (WorkerSummary, error) {
	var wire struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return WorkerSummary{}, err
	}
	if wire.ID != requestedWorkerID {
		return WorkerSummary{}, fmt.Errorf("worker identity does not match request")
	}
	return WorkerSummary{ID: requestedWorkerID, Title: wire.Title, Status: wire.Status}, nil
}

func workerFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "rippling", Operation: "getWorker", Message: message}
}

func workerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WorkerSummary] {
	failure := workerFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, WorkerSummary{}, &failure, sdkgo.Receipt{})
}
