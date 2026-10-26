// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"hiverDispatchedCallId"`
	CanResend        bool         `json:"canResend,omitempty"`
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// sendOnce sends only with a valid first-attempt state or a matching safe-resend marker.
func sendOnce[OUT any](call sdkgo.Call, client *Client, credentials Credentials, operation string, request hiverRequest, branches singleDispatchBranches) (hiverExchange, *sdkgo.MutationAttempt[OUT]) {
	if priorAttempt := previousSingleDispatchAttempt[OUT](call, client, operation); priorAttempt != nil {
		return hiverExchange{}, priorAttempt
	}
	available := 30 * time.Second
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		available = min(available, max(time.Until(deadline), 0))
	}
	minimumSendTime := max(available/10, 100*time.Millisecond)
	if result, isAvailable := client.awaitRequestSlot(call, operation); !isAvailable {
		if err := recordSingleDispatchRetry(call); err != nil {
			attempt := sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, operation, "Dex did not record the not-sent checkpoint"), 0)
			return result, &attempt
		}
		attempt := sdkgo.NewMutationRetry[OUT](result.failure, 0)
		return result, &attempt
	}
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline && time.Until(deadline) < minimumSendTime {
		if err := recordSingleDispatchRetry(call); err != nil {
			attempt := sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, operation, "Dex did not record the not-sent checkpoint"), 0)
			return hiverExchange{}, &attempt
		}
		attempt := sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, operation, "too little Step time remained to send the Hiver request"), 0)
		return hiverExchange{}, &attempt
	}
	if err := call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID}); err != nil {
		attempt := sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, operation, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
		return hiverExchange{}, &attempt
	}
	request.correlationCallID = call.ID
	result := client.send(call, credentials, operation, request)
	if attempt, isTerminal := singleDispatchAttemptForExchange[OUT](call, result, client.receipt(call, result.response, ""), branches); isTerminal {
		return result, &attempt
	}
	return result, nil
}

func previousSingleDispatchAttempt[OUT any](call sdkgo.Call, client *Client, operation string) *sdkgo.MutationAttempt[OUT] {
	if canSendSingleDispatch(call) {
		return nil
	}
	var zero OUT
	attempt := sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureTransport, operation,
		"an earlier attempt of this Step may have sent the request, so it is not sent again"), client.receipt(call, hiverResponse{}, ""))
	return &attempt
}

// canSendSingleDispatch requires an acknowledged not-sent marker on every later attempt.
func canSendSingleDispatch(call sdkgo.Call) bool {
	var marker singleDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err != nil {
		return false
	}
	if call.Context.Attempt() == 1 {
		return !isMarkerFound
	}
	return isMarkerFound && marker.DispatchedCallID == call.ID && marker.CanResend
}

// singleDispatchAttemptForExchange maps a sent request: only a provable non-application is retried.
func singleDispatchAttemptForExchange[OUT any](call sdkgo.Call, result hiverExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		if err := recordSingleDispatchRetry(call); err != nil {
			return sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureAvailability, call.Operation.OperationID, "Dex did not record the safe retry checkpoint"), receipt), true
		}
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeUnavailable, exchangeInvalid:
		if result.response.statusCode == http.StatusServiceUnavailable {
			if err := recordSingleDispatchRetry(call); err != nil {
				return sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureAvailability, call.Operation.OperationID, "Dex did not record the safe retry checkpoint"), receipt), true
			}
			return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
		}
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt), true
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(branches.notFound, zero, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(branches.defect, zero, &result.failure, receipt), true
	default:
		if result.response.statusCode == http.StatusConflict {
			return sdkgo.NewMutationUncertain(zero, result.failure, receipt), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	}
}

func recordSingleDispatchRetry(call sdkgo.Call) error {
	return call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID, CanResend: true})
}
