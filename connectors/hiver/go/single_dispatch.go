// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const singleDispatchExecuteTimeout = 30 * time.Second

type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
	// providerIDField is the body field holding the created object's ID.
	providerIDField string
}

// singleDispatchPermit is a fence that allows this attempt to send, with its send-cap facts.
type singleDispatchPermit struct {
	fence         *singledispatch.Fence
	pacerDeadline time.Time
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
	// canResendUnknownOutcome means this attempt's send leaves room under the cap for one more.
	canResendUnknownOutcome bool
}

type hiverSendAttempt struct {
	client      *Client
	credentials Credentials
	operation   string
	request     hiverRequest
	result      hiverExchange
}

func (attempt *hiverSendAttempt) send(ctx context.Context) error {
	attempt.result = attempt.client.sendWithContext(ctx, attempt.credentials, attempt.operation, attempt.request, true)
	return nil
}

// beginSingleDispatch decides whether this attempt may send. Hiver has no note or draft
// read-back, so an earlier possible send is resent while the send cap allows it.
func beginSingleDispatch[OUT any](call sdkgo.Call, client *Client, operation string, defectBranch sdkgo.BranchID) (singleDispatchPermit, *sdkgo.MutationAttempt[OUT]) {
	var zero OUT
	startedAt := time.Now()
	budget := singleDispatchExecuteTimeout
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		budget = min(budget, max(deadline.Sub(startedAt), 0))
	}
	// No read-back exists; 5/6 leaves time to classify a response after the request ends.
	fence, err := singledispatch.NewFence(call, singleDispatchExecuteTimeout,
		singledispatch.WithWriteWindow(5.0/6.0), singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		attempt := sdkgo.NewMutationBranch(defectBranch, zero,
			hiverFailurePointer(sdkgo.FailureLocalDefect, operation, "single-dispatch fence could not start"), sdkgo.Receipt{})
		return singleDispatchPermit{}, &attempt
	}
	decision := fence.Decide()
	if decision.Kind != singledispatch.FailClosed && decision.Marker.ProviderID != "" {
		// A saved provider ID proves Hiver accepted the write; Hiver has no note or draft read by ID.
		attempt := sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureTransport, operation,
			"Hiver accepted an earlier attempt's write, and its answer was lost"), client.receipt(call, hiverResponse{}, decision.Marker.ProviderID))
		return singleDispatchPermit{}, &attempt
	}
	switch decision.Kind {
	case singledispatch.SendAllowed, singledispatch.ReadBackThenResendAllowed:
	case singledispatch.ReadBackRequired:
		attempt := sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureTransport, operation,
			"earlier attempts may have sent the request and the send cap is reached, so it is not sent again"), client.receipt(call, hiverResponse{}, decision.Marker.ProviderID))
		return singleDispatchPermit{}, &attempt
	default:
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		attempt := sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, operation,
			"the single-dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
		return singleDispatchPermit{}, &attempt
	}
	return singleDispatchPermit{
		fence: fence,
		// Half the initial budget ends before the fence's write window minus minimum send time.
		pacerDeadline:           startedAt.Add(budget / 2),
		isFirstSend:             !hasPossiblePriorSend(decision),
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
	}, nil
}

// hasPossiblePriorSend also covers SendAllowed after a canResend marker that still counts earlier sends.
func hasPossiblePriorSend(decision singledispatch.Decision) bool {
	return decision.Kind != singledispatch.SendAllowed || decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != ""
}

// retryBeforeSingleDispatch returns Retry for a failure before Dispatch.
func retryBeforeSingleDispatch[OUT any](permit singleDispatchPermit, failure sdkgo.Failure) sdkgo.MutationAttempt[OUT] {
	// After a possible earlier send, the fence records nothing, so the next attempt counts that send.
	if permit.isFirstSend {
		if err := permit.fence.RecordNotSent(); err != nil {
			failure = hiverFailure(sdkgo.FailureAvailability, failure.Operation, "Dex did not record the not-sent checkpoint")
		}
	}
	return sdkgo.NewMutationRetry[OUT](failure, 0)
}

// credentialFailureBeforeSingleDispatch retries a transient failure; a terminal one selects defect only on a first send.
func credentialFailureBeforeSingleDispatch[OUT any](call sdkgo.Call, client *Client, permit singleDispatchPermit, failure sdkgo.Failure,
	isTerminal bool, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	if !isTerminal {
		return retryBeforeSingleDispatch[OUT](permit, failure)
	}
	var zero OUT
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(zero, hiverFailure(failure.Kind, failure.Operation,
			"an earlier attempt may have sent the request and the connection credentials are unusable"), client.receipt(call, hiverResponse{}, ""))
	}
	if err := permit.fence.RecordNotSent(); err != nil {
		return sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, failure.Operation, "Dex did not record the not-sent checkpoint"), 0)
	}
	return sdkgo.NewMutationBranch(defectBranch, zero, &failure, sdkgo.Receipt{})
}

func sendOnce[OUT any](call sdkgo.Call, client *Client, credentials Credentials, operation string, request hiverRequest,
	branches singleDispatchBranches, permit singleDispatchPermit) (hiverExchange, *sdkgo.MutationAttempt[OUT]) {
	waitContext, cancel := context.WithDeadline(call.Context, permit.pacerDeadline)
	result, isAvailable := client.awaitRequestSlotWithContext(waitContext, operation)
	cancel()
	if !isAvailable {
		attempt := retryBeforeSingleDispatch[OUT](permit, result.failure)
		return result, &attempt
	}
	request.correlationCallID = call.ID
	sendAttempt := &hiverSendAttempt{client: client, credentials: credentials, operation: operation, request: request}
	if err := permit.fence.Dispatch(sendAttempt.send); err != nil {
		// Dispatch refused before sending, so nothing reached Hiver in this attempt.
		attempt := sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, operation, "Hiver write was not dispatched"), 0)
		return hiverExchange{}, &attempt
	}
	result = sendAttempt.result
	if attempt, isTerminal := singleDispatchAttemptForExchange[OUT](result, client.receipt(call, result.response, ""), branches, permit); isTerminal {
		return result, &attempt
	}
	return result, nil
}

// singleDispatchAttemptForExchange returns the attempt for every outcome except success.
// Only a definite refusal selects a rejection branch; any other outcome may have been applied.
func singleDispatchAttemptForExchange[OUT any](result hiverExchange, receipt sdkgo.Receipt,
	branches singleDispatchBranches, permit singleDispatchPermit) (sdkgo.MutationAttempt[OUT], bool) {
	if result.outcome != exchangeSucceeded && isSuccessStatus(result.response.statusCode) {
		// Hiver accepted the write, but its answer is oversized, unreadable, or untrusted.
		return acceptedWriteAttempt[OUT](permit, "", result.failure, receipt), true
	}
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		return retryAfterSingleDispatch[OUT](result, permit), true
	case exchangeNotFound:
		return refusalAfterSingleDispatch[OUT](result, receipt, branches.notFound, branches, permit), true
	case exchangeDefect:
		// The request could not be built, so a resend would fail the same way.
		return rejectAfterSingleDispatch[OUT](result, receipt, branches.defect, permit), true
	case exchangeRejected:
		if isDefiniteRefusalStatus(result.response.statusCode) {
			return refusalAfterSingleDispatch[OUT](result, receipt, branches.providerRejected, branches, permit), true
		}
	default:
		if result.response.statusCode == http.StatusServiceUnavailable {
			return retryAfterSingleDispatch[OUT](result, permit), true
		}
	}
	return resendOrSelectUncertain[OUT](permit, result.failure, result.retryAfter, receipt), true
}

// isDefiniteRefusalStatus reports a status that proves Hiver applied nothing.
func isDefiniteRefusalStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

// retryAfterSingleDispatch retries a send Hiver did not process.
func retryAfterSingleDispatch[OUT any](result hiverExchange, permit singleDispatchPermit) sdkgo.MutationAttempt[OUT] {
	// A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
	_ = permit.fence.RecordCanResend()
	return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
}

// refusalAfterSingleDispatch treats a refusal status as unknown when its body is unreadable,
// and as accepted when its body carries the created object's ID.
func refusalAfterSingleDispatch[OUT any](result hiverExchange, receipt sdkgo.Receipt, branch sdkgo.BranchID,
	branches singleDispatchBranches, permit singleDispatchPermit) sdkgo.MutationAttempt[OUT] {
	status := result.response.statusCode
	if result.isErrorBodyUnreadable {
		return resendOrSelectUncertain[OUT](permit, hiverFailure(sdkgo.FailureTransport, result.failure.Operation,
			fmt.Sprintf("Hiver answered HTTP %d, but its oversized or unreadable body may hide an accepted write", status)), 0, receipt)
	}
	if providerID := acceptedProviderID(result.response.body, branches.providerIDField); providerID != "" {
		receipt.ProviderObjectID = providerID
		return acceptedWriteAttempt[OUT](permit, providerID, hiverFailure(sdkgo.FailureProtocol, result.failure.Operation,
			fmt.Sprintf("Hiver answered HTTP %d with the ID of a created object, so the write may have been applied", status)), receipt)
	}
	return rejectAfterSingleDispatch[OUT](result, receipt, branch, permit)
}

// rejectAfterSingleDispatch selects a refusal branch only when no earlier send may have applied.
func rejectAfterSingleDispatch[OUT any](result hiverExchange, receipt sdkgo.Receipt, branch sdkgo.BranchID, permit singleDispatchPermit) sdkgo.MutationAttempt[OUT] {
	if !permit.isFirstSend {
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt)
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[OUT](hiverFailure(sdkgo.FailureAvailability, result.failure.Operation,
			"Dex did not record the refusal checkpoint"), 0)
	}
	var zero OUT
	return sdkgo.NewMutationBranch(branch, zero, &result.failure, receipt)
}

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func resendOrSelectUncertain[OUT any](permit singleDispatchPermit, failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if permit.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[OUT](failure, retryAfter)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, failure, receipt)
}

// acceptedProviderID returns the valid ID field of a single-data body, or "" when it has none.
func acceptedProviderID(body []byte, field string) string {
	raw, err := decodeSingleData(body)
	if err != nil {
		return ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	var providerID wireID
	if json.Unmarshal(fields[field], &providerID) != nil || !hiverIDPattern.MatchString(string(providerID)) {
		return ""
	}
	return string(providerID)
}

// acceptedWriteAttempt handles a 2xx answer that cannot be used. Hiver accepted the write and
// offers no note or draft read-back, so it is never resent and selects uncertain.
func acceptedWriteAttempt[OUT any](permit singleDispatchPermit, providerID string, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if providerID != "" {
		// The claim marker already forbids a resend if this identity checkpoint is lost.
		_ = permit.fence.RecordProviderID(providerID)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, failure, receipt)
}

func isSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}
