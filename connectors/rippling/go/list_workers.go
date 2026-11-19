// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rippling

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListWorkersInput selects one page of visible worker summaries.
type ListWorkersInput struct {
	// Limit is the number of workers to return, from 1 to 100. Zero uses Rippling's default.
	Limit int `json:"limit,omitempty"`
	// Cursor is the opaque pagination cursor returned by an earlier page.
	Cursor string `json:"cursor,omitempty"`
}

// WorkerPage contains visible worker summaries and the next opaque cursor.
type WorkerPage struct {
	// Workers contains only IDs, statuses, and titles visible to the token owner.
	Workers []WorkerSummary `json:"workers"`
	// NextCursor is empty when Rippling reports no further page.
	NextCursor string `json:"nextCursor"`
}

// ListWorkersOperation reads one page of workers.
type ListWorkersOperation struct{ client *Client }

// ListWorkers binds the Query to this client.
func (client *Client) ListWorkers() ListWorkersOperation { return ListWorkersOperation{client} }

// Definition returns the manifest-generated Query definition.
func (ListWorkersOperation) Definition() sdkgo.QueryDefinition { return ListWorkersDefinition }

// Invoke reads one page and classifies provider outcomes.
func (operation ListWorkersOperation) Invoke(call sdkgo.Call, input ListWorkersInput) sdkgo.QueryAttempt[WorkerPage] {
	if input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 2048 {
		return listWorkersBranch(ListWorkersBranchDefect, sdkgo.FailureValidation, "invalid worker page limit or cursor")
	}
	query := url.Values{}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	path := "/workers/"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return listWorkersBranch(ListWorkersBranchDefect, sdkgo.FailureAuthentication, "Rippling credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[WorkerPage](listWorkersFailure(sdkgo.FailureTransport, "Rippling request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[WorkerPage](listWorkersFailure(sdkgo.FailureRateLimit, "Rippling rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[WorkerPage](listWorkersFailure(sdkgo.FailureAvailability, "Rippling is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listWorkersBranch(ListWorkersBranchProviderRejected, sdkgo.FailureProviderRejection, "Rippling rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listWorkersBranch(ListWorkersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Rippling response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[WorkerPage](listWorkersFailure(sdkgo.FailureTransport, "Rippling response was interrupted"), 0)
	}
	var wire struct {
		Results []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Title  string `json:"title"`
		} `json:"results"`
		NextLink string `json:"next_link"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Results == nil {
		return listWorkersBranch(ListWorkersBranchInvalidResponse, sdkgo.FailureProtocol, "Rippling returned invalid worker page JSON")
	}
	page := WorkerPage{Workers: make([]WorkerSummary, 0, len(wire.Results))}
	for _, worker := range wire.Results {
		if !validWorkerID(worker.ID) {
			return listWorkersBranch(ListWorkersBranchInvalidResponse, sdkgo.FailureProtocol, "Rippling returned an invalid worker ID")
		}
		page.Workers = append(page.Workers, WorkerSummary{ID: worker.ID, Status: worker.Status, Title: worker.Title})
	}
	if wire.NextLink != "" {
		nextURL, err := url.Parse(wire.NextLink)
		if err != nil || nextURL.Query().Get("cursor") == "" {
			return listWorkersBranch(ListWorkersBranchInvalidResponse, sdkgo.FailureProtocol, "Rippling returned an invalid worker page link")
		}
		page.NextCursor = nextURL.Query().Get("cursor")
	}
	return sdkgo.NewQueryBranch(ListWorkersBranchFound, page, nil, sdkgo.Receipt{})
}

func listWorkersFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "rippling", Operation: "listWorkers", Message: message}
}

func listWorkersBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WorkerPage] {
	failure := listWorkersFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, WorkerPage{}, &failure, sdkgo.Receipt{})
}
