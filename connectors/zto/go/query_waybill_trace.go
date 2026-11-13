// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zto

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

// QueryWaybillTraceInput identifies a ZTO waybill under the application's bound electronic-waybill account.
type QueryWaybillTraceInput struct {
	// BillCode is the ZTO waybill number.
	BillCode string `json:"billCode"`
}

// WaybillEvent is one tracking scan with provider descriptions and personal details omitted.
type WaybillEvent struct {
	// ScanType is the ZTO scan type, such as 收件 or 签收.
	ScanType string `json:"scanType"`
	// ScanDate is the Unix timestamp in milliseconds.
	ScanDate int64 `json:"scanDate"`
	// ScanSiteName is the scanning site's name, when available.
	ScanSiteName string `json:"scanSiteName,omitempty"`
}

// WaybillTrace contains a bounded, privacy-minimized tracking history.
type WaybillTrace struct {
	// BillCode is the queried waybill number.
	BillCode string `json:"billCode"`
	// Events follow the order returned by ZTO.
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

// Invoke queries ZTO and classifies the provider outcome.
func (operation QueryWaybillTraceOperation) Invoke(call sdkgo.Call, input QueryWaybillTraceInput) sdkgo.QueryAttempt[WaybillTrace] {
	response := operation.client.queryWaybillTrace(call, input)
	if errors.Is(response.err, errInput) {
		return traceBranch(QueryWaybillTraceBranchDefect, sdkgo.FailureValidation, "ZTO waybill input is invalid")
	}
	if errors.Is(response.err, errCredential) {
		return traceBranch(QueryWaybillTraceBranchDefect, sdkgo.FailureAuthentication, "ZTO credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureTransport, "ZTO request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureRateLimit, "ZTO rate limited the query"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureAvailability, "ZTO is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ZTO response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureTransport, "ZTO response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return traceBranch(QueryWaybillTraceBranchProviderRejected, sdkgo.FailureProviderRejection, "ZTO rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "ZTO returned invalid UTF-8")
	}
	var wire struct {
		Status     *bool  `json:"status"`
		StatusCode string `json:"statusCode"`
		Message    string `json:"message"`
		Result     *[]struct {
			BillCode string `json:"billCode"`
			ScanType string `json:"scanType"`
			ScanDate int64  `json:"scanDate"`
			ScanSite *struct {
				Name string `json:"name"`
			} `json:"scanSite"`
		} `json:"result"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "ZTO returned invalid tracking JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "ZTO returned trailing JSON")
	}
	if wire.Status == nil || wire.StatusCode == "" {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "ZTO returned incomplete status")
	}
	if !*wire.Status {
		switch wire.StatusCode {
		case "E500":
			return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureAvailability, "ZTO reported a system error"), 0)
		}
		return traceBranch(QueryWaybillTraceBranchProviderRejected, sdkgo.FailureProviderRejection, "ZTO rejected code "+wire.StatusCode)
	}
	if wire.StatusCode != "SYS000" || wire.Result == nil || len(*wire.Result) > 1000 {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "ZTO returned incomplete tracking history")
	}
	events := make([]WaybillEvent, 0, len(*wire.Result))
	for _, scan := range *wire.Result {
		if scan.BillCode != "" && scan.BillCode != input.BillCode {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "ZTO returned a different waybill")
		}
		if scan.ScanType == "" || scan.ScanDate <= 0 {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "ZTO returned incomplete scan")
		}
		event := WaybillEvent{ScanType: scan.ScanType, ScanDate: scan.ScanDate}
		if scan.ScanSite != nil {
			event.ScanSiteName = scan.ScanSite.Name
		}
		events = append(events, event)
	}
	return sdkgo.NewQueryBranch(QueryWaybillTraceBranchFound, WaybillTrace{BillCode: input.BillCode, Events: events}, nil, sdkgo.Receipt{})
}
func traceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "zto", Operation: "queryWaybillTrace", Message: message}
}
func traceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WaybillTrace] {
	failure := traceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, WaybillTrace{}, &failure, sdkgo.Receipt{})
}
