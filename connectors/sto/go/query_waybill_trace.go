// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sto

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

// QueryWaybillTraceInput identifies one STO waybill authorized for the subscription application.
type QueryWaybillTraceInput struct {
	// WaybillNo is the STO waybill number.
	WaybillNo string `json:"waybillNo"`
}

// WaybillEvent is one tracking scan with personal details omitted.
type WaybillEvent struct {
	// ScanType is the provider's scan type, such as 收件 or 签收.
	ScanType string `json:"scanType"`
	// OpTime is the scan time in yyyy-MM-dd HH:mm:ss format.
	OpTime string `json:"opTime"`
	// OpOrgName is the scanning branch name, when available.
	OpOrgName string `json:"opOrgName,omitempty"`
}

// WaybillTrace contains a bounded, privacy-minimized tracking history.
type WaybillTrace struct {
	// WaybillNo is the queried waybill number.
	WaybillNo string `json:"waybillNo"`
	// Events follow the order returned by STO.
	Events []WaybillEvent `json:"events"`
}

// QueryWaybillTraceOperation reads one authorized waybill's tracking scans.
type QueryWaybillTraceOperation struct{ client *Client }

// QueryWaybillTrace binds the Query to this client.
func (client *Client) QueryWaybillTrace() QueryWaybillTraceOperation {
	return QueryWaybillTraceOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (QueryWaybillTraceOperation) Definition() sdkgo.QueryDefinition {
	return QueryWaybillTraceDefinition
}

// Invoke queries STO and classifies the provider outcome.
func (operation QueryWaybillTraceOperation) Invoke(call sdkgo.Call, input QueryWaybillTraceInput) sdkgo.QueryAttempt[WaybillTrace] {
	response := operation.client.queryWaybillTrace(call, input)
	if errors.Is(response.err, errInput) {
		return traceBranch(QueryWaybillTraceBranchDefect, sdkgo.FailureValidation, "STO waybill input is invalid")
	}
	if errors.Is(response.err, errCredential) {
		return traceBranch(QueryWaybillTraceBranchDefect, sdkgo.FailureAuthentication, "STO credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureTransport, "STO request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureRateLimit, "STO rate limited the query"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureAvailability, "STO is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "STO response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureTransport, "STO response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return traceBranch(QueryWaybillTraceBranchProviderRejected, sdkgo.FailureProviderRejection, "STO rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO returned invalid UTF-8")
	}
	var wire struct {
		Success   *bool  `json:"success"`
		ErrorCode string `json:"errorCode"`
		NeedRetry bool   `json:"needRetry"`
		Data      map[string][]struct {
			WaybillNo string `json:"waybillNo"`
			ScanType  string `json:"scanType"`
			OpTime    string `json:"opTime"`
			OpOrgName string `json:"opOrgName"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO returned invalid tracking JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO returned trailing JSON")
	}
	if wire.Success == nil {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO returned incomplete status")
	}
	if !*wire.Success {
		if wire.NeedRetry || isTransientProviderCode(wire.ErrorCode) {
			return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureAvailability, "STO reported a transient error"), 0)
		}
		return traceBranch(QueryWaybillTraceBranchProviderRejected, sdkgo.FailureProviderRejection, "STO rejected the query")
	}
	if wire.Data == nil || len(wire.Data) > 1 {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO returned incomplete tracking map")
	}
	for number := range wire.Data {
		if number != input.WaybillNo {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO returned a different waybill")
		}
	}
	scans := wire.Data[input.WaybillNo]
	if len(scans) > 1000 {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO returned too many scans")
	}
	events := make([]WaybillEvent, 0, len(scans))
	for _, scan := range scans {
		if scan.WaybillNo != "" && scan.WaybillNo != input.WaybillNo {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO scan referenced a different waybill")
		}
		if scan.ScanType == "" || scan.OpTime == "" {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "STO returned incomplete scan")
		}
		events = append(events, WaybillEvent{ScanType: scan.ScanType, OpTime: scan.OpTime, OpOrgName: scan.OpOrgName})
	}
	return sdkgo.NewQueryBranch(QueryWaybillTraceBranchFound, WaybillTrace{WaybillNo: input.WaybillNo, Events: events}, nil, sdkgo.Receipt{})
}
func isTransientProviderCode(code string) bool {
	switch code {
	case "S04", "S06", "S07", "S20", "S30", "S69", "S302":
		return true
	}
	return false
}
func traceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "sto", Operation: "queryWaybillTrace", Message: message}
}
func traceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WaybillTrace] {
	failure := traceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, WaybillTrace{}, &failure, sdkgo.Receipt{})
}
