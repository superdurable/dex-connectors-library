// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"context"
	"encoding/json"
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
	switch decision.Kind {
	case singledispatch.SendAllowed:
	case singledispatch.ReadBackThenResendAllowed:
		// A saved provider ID proves Hiver accepted the write, so it is never resent.
		if decision.Marker.ProviderID != "" {
			attempt := sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureTransport, operation,
				"Hiver accepted an earlier attempt's write, and its answer was lost"), client.receipt(call, hiverResponse{}, decision.Marker.ProviderID))
			return singleDispatchPermit{}, &attempt
		}
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
		isFirstSend:             decision.MaximumPriorSends == 0,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
	}, nil
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
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		return retryAfterSingleDispatch[OUT](result, permit), true
	case exchangeNotFound:
		return rejectAfterSingleDispatch[OUT](result, receipt, branches.notFound, permit), true
	case exchangeDefect:
		if !permit.isFirstSend {
			// defect would hide an earlier possible send.
			return resendOrSelectUncertain[OUT](permit, result.failure, 0, receipt), true
		}
		return rejectAfterSingleDispatch[OUT](result, receipt, branches.defect, permit), true
	case exchangeRejected:
		if isDefiniteRefusalStatus(result.response.statusCode) {
			return rejectAfterSingleDispatch[OUT](result, receipt, branches.providerRejected, permit), true
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

// acceptedProviderID returns the valid ID field of a 2xx single-data body, or "" when it has none.
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

// acceptedWriteAttempt handles a 2xx body that failed to decode.
// A decoded provider ID proves acceptance, so only an ID-less answer is resent.
func acceptedWriteAttempt[OUT any](permit singleDispatchPermit, providerID string, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if providerID != "" {
		// The accepted write is known even if the identity checkpoint is lost.
		_ = permit.fence.RecordProviderID(providerID)
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, failure, receipt)
	}
	return resendOrSelectUncertain[OUT](permit, failure, 0, receipt)
}
