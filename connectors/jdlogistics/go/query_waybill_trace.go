// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jdlogistics

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// QueryWaybillTraceInput identifies one JD Logistics B2C merchant waybill.
type QueryWaybillTraceInput struct {
	// WaybillNo is the authorized JD Logistics waybill number.
	WaybillNo string `json:"waybillNo"`
}

// WaybillEvent is one scan with personal details omitted.
type WaybillEvent struct {
	// State is the provider's tracking status code.
	State string `json:"state"`
	// CategoryName is the tracking stage description.
	CategoryName string `json:"categoryName"`
	// OperationTime is the scan time in yyyy-MM-dd HH:mm:ss format.
	OperationTime string `json:"operationTime"`
}

// WaybillTrace contains a bounded, privacy-minimized tracking history.
type WaybillTrace struct {
	// WaybillNo is the queried waybill number.
	WaybillNo string `json:"waybillNo"`
	// Events follow the order returned by JD Logistics.
	Events []WaybillEvent `json:"events"`
}

// QueryWaybillTraceOperation reads one authorized merchant waybill's tracking scans.
type QueryWaybillTraceOperation struct{ client *Client }

// QueryWaybillTrace binds the Query to this client.
func (client *Client) QueryWaybillTrace() QueryWaybillTraceOperation {
	return QueryWaybillTraceOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (QueryWaybillTraceOperation) Definition() sdkgo.QueryDefinition {
	return QueryWaybillTraceDefinition
}

// Invoke queries JD Logistics and classifies the provider outcome.
func (operation QueryWaybillTraceOperation) Invoke(call sdkgo.Call, input QueryWaybillTraceInput) sdkgo.QueryAttempt[WaybillTrace] {
	response := operation.client.queryWaybillTrace(call, input)
	if errors.Is(response.err, errInput) {
		return traceBranch(QueryWaybillTraceBranchDefect, sdkgo.FailureValidation, "JD Logistics waybill input is invalid")
	}
	if errors.Is(response.err, errCredential) {
		return traceBranch(QueryWaybillTraceBranchDefect, sdkgo.FailureAuthentication, "JD Logistics credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureTransport, "JD Logistics request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureRateLimit, "JD Logistics rate limited the query"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureAvailability, "JD Logistics is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "JD Logistics response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureTransport, "JD Logistics response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return traceBranch(QueryWaybillTraceBranchProviderRejected, sdkgo.FailureProviderRejection, "JD Logistics rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "JD Logistics returned invalid UTF-8")
	}
	var wire struct {
		Success *bool `json:"success"`
		Code    *int  `json:"code"`
		Data    *struct {
			TraceDetails []struct {
				State         string `json:"state"`
				CategoryName  string `json:"categoryName"`
				OperationTime string `json:"operationTime"`
				WaybillCode   string `json:"waybillCode"`
			} `json:"traceDetails"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "JD Logistics returned invalid tracking JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "JD Logistics returned trailing JSON")
	}
	if wire.Success == nil || wire.Code == nil {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "JD Logistics returned incomplete status")
	}
	if !*wire.Success || *wire.Code != 0 {
		if *wire.Code >= 4000 && *wire.Code < 5000 {
			return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureAvailability, "JD Logistics reported a transient service error"), 0)
		}
		return traceBranch(QueryWaybillTraceBranchProviderRejected, sdkgo.FailureProviderRejection, "JD Logistics rejected the query")
	}
	if wire.Data == nil || len(wire.Data.TraceDetails) > 1000 {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "JD Logistics returned incomplete or excessive scans")
	}
	events := make([]WaybillEvent, 0, len(wire.Data.TraceDetails))
	for _, scan := range wire.Data.TraceDetails {
		if scan.WaybillCode != "" && scan.WaybillCode != input.WaybillNo {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "JD Logistics returned a different waybill")
		}
		if scan.State == "" || scan.OperationTime == "" {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "JD Logistics returned incomplete scan")
		}
		events = append(events, WaybillEvent{State: scan.State, CategoryName: scan.CategoryName, OperationTime: scan.OperationTime})
	}
	return sdkgo.NewQueryBranch(QueryWaybillTraceBranchFound, WaybillTrace{WaybillNo: input.WaybillNo, Events: events}, nil, sdkgo.Receipt{})
}
func traceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "jdlogistics", Operation: "queryWaybillTrace", Message: message}
}
func traceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WaybillTrace] {
	failure := traceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, WaybillTrace{}, &failure, sdkgo.Receipt{})
}
