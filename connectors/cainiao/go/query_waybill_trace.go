// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cainiao

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

// QueryWaybillTraceInput identifies a Cainiao electronic waybill and carrier.
type QueryWaybillTraceInput struct {
	// CarrierCode is the carrier cp_code, such as YTO.
	CarrierCode string `json:"carrierCode"`
	// WaybillCode is the Cainiao electronic waybill number.
	WaybillCode string `json:"waybillCode"`
}

// WaybillEvent is one logistics event returned by TOP.
type WaybillEvent struct {
	// Action is the provider's event action code.
	Action string `json:"action"`
	// Description is the provider's event description.
	Description string `json:"description"`
	// Time is the provider's event timestamp in yyyy-MM-dd HH:mm:ss format.
	Time string `json:"time"`
}

// WaybillTrace contains bounded event history for one electronic waybill.
type WaybillTrace struct {
	// CarrierCode is the queried cp_code.
	CarrierCode string `json:"carrierCode"`
	// WaybillCode is the queried electronic waybill number.
	WaybillCode string `json:"waybillCode"`
	// Events are logistics events in the order returned by TOP.
	Events []WaybillEvent `json:"events"`
}

// QueryWaybillTraceOperation reads one electronic waybill's logistics events.
type QueryWaybillTraceOperation struct{ client *Client }

// QueryWaybillTrace binds the Query to this client.
func (client *Client) QueryWaybillTrace() QueryWaybillTraceOperation {
	return QueryWaybillTraceOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (QueryWaybillTraceOperation) Definition() sdkgo.QueryDefinition {
	return QueryWaybillTraceDefinition
}

// Invoke queries TOP and classifies the provider outcome.
func (operation QueryWaybillTraceOperation) Invoke(call sdkgo.Call, input QueryWaybillTraceInput) sdkgo.QueryAttempt[WaybillTrace] {
	response := operation.client.queryWaybillTrace(call, input)
	if errors.Is(response.err, errInput) {
		return traceBranch(QueryWaybillTraceBranchDefect, sdkgo.FailureValidation, "Cainiao waybill input is invalid")
	}
	if errors.Is(response.err, errCredential) {
		return traceBranch(QueryWaybillTraceBranchDefect, sdkgo.FailureAuthentication, "Cainiao credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureTransport, "Cainiao request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureRateLimit, "Cainiao rate limited the query"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureAvailability, "Cainiao is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Cainiao response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[WaybillTrace](traceFailure(sdkgo.FailureTransport, "Cainiao response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return traceBranch(QueryWaybillTraceBranchProviderRejected, sdkgo.FailureProviderRejection, "Cainiao rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "Cainiao returned invalid UTF-8")
	}
	var wire struct {
		Error *struct {
			Code    json.Number `json:"code"`
			Message string      `json:"msg"`
			SubCode string      `json:"sub_code"`
		} `json:"error_response"`
		Success *struct {
			List *struct {
				Events []WaybillEvent `json:"logistics_detail_dto"`
			} `json:"logistics_detail_dto_list"`
		} `json:"cainiao_waybill_logisticstrace_get_response"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	decoder.UseNumber()
	if decoder.Decode(&wire) != nil {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "Cainiao returned invalid trace JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "Cainiao returned trailing JSON")
	}
	if wire.Error != nil {
		if wire.Success != nil {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "Cainiao returned conflicting outcomes")
		}
		if wire.Error.Code.String() == "" {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "Cainiao returned incomplete error")
		}
		return traceBranch(QueryWaybillTraceBranchProviderRejected, sdkgo.FailureProviderRejection, "Cainiao rejected code "+wire.Error.Code.String())
	}
	if wire.Success == nil || wire.Success.List == nil || wire.Success.List.Events == nil || len(wire.Success.List.Events) > 1000 {
		return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "Cainiao returned incomplete trace")
	}
	for _, event := range wire.Success.List.Events {
		if event.Action == "" || event.Description == "" || event.Time == "" {
			return traceBranch(QueryWaybillTraceBranchInvalidResponse, sdkgo.FailureProtocol, "Cainiao returned incomplete event")
		}
	}
	return sdkgo.NewQueryBranch(QueryWaybillTraceBranchFound, WaybillTrace{CarrierCode: input.CarrierCode, WaybillCode: input.WaybillCode, Events: wire.Success.List.Events}, nil, sdkgo.Receipt{})
}
func traceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "cainiao", Operation: "queryWaybillTrace", Message: message}
}
func traceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[WaybillTrace] {
	failure := traceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, WaybillTrace{}, &failure, sdkgo.Receipt{})
}
