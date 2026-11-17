// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// freshdeskWritePermit is a fence that allows this attempt to send, with its send-cap facts.
type freshdeskWritePermit struct {
	fence *singledispatch.Fence
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
	// canResendUnknownOutcome means this attempt's send leaves room under the cap for one more.
	canResendUnknownOutcome bool
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// beginFreshdeskWrite decides whether this attempt may send. Freshdesk has no note or ticket
// read-back by Call ID, so an earlier possible send is resent while the send cap allows it.
func beginFreshdeskWrite[OUT any](call sdkgo.Call, client *Client, operation string, executeTimeout time.Duration, defect sdkgo.BranchID) (freshdeskWritePermit, *sdkgo.MutationAttempt[OUT]) {
	var zero OUT
	fence, err := singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		attempt := sdkgo.NewMutationBranch(defect, zero, freshdeskFailurePointer(sdkgo.FailureLocalDefect, operation, "single-dispatch fence could not start"), sdkgo.Receipt{})
		return freshdeskWritePermit{}, &attempt
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.SendAllowed:
	case singledispatch.ReadBackThenResendAllowed:
		// A saved provider ID proves Freshdesk accepted the write, so it is never resent.
		if decision.Marker.ProviderID != "" {
			attempt := sdkgo.NewMutationUncertain(zero, freshdeskFailure(sdkgo.FailureTransport, operation,
				"Freshdesk accepted an earlier attempt's write, and its answer was lost"), client.receipt(call, freshdeskResponse{}, markerProviderID(decision.Marker)))
			return freshdeskWritePermit{}, &attempt
		}
	case singledispatch.ReadBackRequired:
		attempt := sdkgo.NewMutationUncertain(zero, freshdeskFailure(sdkgo.FailureTransport, operation,
			"earlier attempts may have sent the request and the send cap is reached, so it is not sent again"), client.receipt(call, freshdeskResponse{}, markerProviderID(decision.Marker)))
		return freshdeskWritePermit{}, &attempt
	default:
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		attempt := sdkgo.NewMutationRetry[OUT](freshdeskFailure(sdkgo.FailureAvailability, operation,
			"the single-dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
		return freshdeskWritePermit{}, &attempt
	}
	return freshdeskWritePermit{
		fence:                   fence,
		isFirstSend:             decision.MaximumPriorSends == 0,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
	}, nil
}

// credentialAttempt maps a credential failure before any send in this attempt.
func credentialAttempt[OUT any](permit freshdeskWritePermit, failure sdkgo.Failure, isTransient bool, defect sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	if !permit.isFirstSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		if isTransient {
			return sdkgo.NewMutationRetry[OUT](failure, 0)
		}
		return sdkgo.NewMutationUncertain(zero, failure, sdkgo.Receipt{})
	}
	if err := permit.fence.RecordNotSent(); err != nil {
		return sdkgo.NewMutationRetry[OUT](freshdeskFailure(sdkgo.FailureAvailability, failure.Operation, "Dex did not record the not-sent checkpoint"), 0)
	}
	if isTransient {
		return sdkgo.NewMutationRetry[OUT](failure, 0)
	}
	return sdkgo.NewMutationBranch(defect, zero, &failure, sdkgo.Receipt{})
}

// sendFreshdeskWrite dispatches one request under the fence; a refused dispatch returns its Retry attempt.
func sendFreshdeskWrite[OUT any](permit freshdeskWritePermit, client *Client, credentials Credentials, operation string,
	request freshdeskRequest, writeBudget freshdeskWriteBudget) (freshdeskExchange, *sdkgo.MutationAttempt[OUT]) {
	var result freshdeskExchange
	if err := permit.fence.Dispatch(func(requestContext context.Context) error {
		result = client.exchangeWithContext(requestContext, credentials, operation, request, writeBudget)
		return nil
	}); err != nil {
		// Dispatch refused before sending, so nothing reached Freshdesk in this attempt.
		attempt := sdkgo.NewMutationRetry[OUT](freshdeskFailure(sdkgo.FailureAvailability, operation, "Freshdesk write was not dispatched"), 0)
		return result, &attempt
	}
	return result, nil
}

// freshdeskWriteAttemptForExchange maps a sent write. Only a definite refusal selects a refusal
// branch; any other outcome may have been applied, so it resends while the send cap allows.
func freshdeskWriteAttemptForExchange[OUT any](permit freshdeskWritePermit, result freshdeskExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeServiceUnavailable:
		// A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
		_ = permit.fence.RecordCanResend()
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeNotFound:
		branch := branches.notFound
		if branch == "" {
			branch = branches.providerRejected
		}
		return rejectFreshdeskWrite[OUT](permit, result, receipt, branch), true
	case exchangeDefect:
		// defect would hide an earlier possible send, so only a first send selects it.
		if permit.isFirstSend {
			return rejectFreshdeskWrite[OUT](permit, result, sdkgo.Receipt{}, branches.defect), true
		}
	case exchangeRejected:
		if isDefiniteRefusalStatus(result.response.statusCode) {
			return rejectFreshdeskWrite[OUT](permit, result, receipt, branches.providerRejected), true
		}
	}
	return resendOrSelectUncertain[OUT](permit, result.failure, result.retryAfter, receipt), true
}

// isDefiniteRefusalStatus reports a status that proves Freshdesk applied nothing.
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

// rejectFreshdeskWrite selects a refusal branch only when no earlier send of this call may have applied.
func rejectFreshdeskWrite[OUT any](permit freshdeskWritePermit, result freshdeskExchange, receipt sdkgo.Receipt, branch sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt)
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[OUT](freshdeskFailure(sdkgo.FailureAvailability, result.failure.Operation, "Dex did not record the refusal checkpoint"), 0)
	}
	return sdkgo.NewMutationBranch(branch, zero, &result.failure, receipt)
}

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func resendOrSelectUncertain[OUT any](permit freshdeskWritePermit, failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if permit.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[OUT](failure, retryAfter)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, failure, receipt)
}

// acceptedWriteAttempt handles a 2xx body that failed to decode.
// A decoded provider ID proves acceptance, so only an ID-less answer is resent.
func acceptedWriteAttempt[OUT any](permit freshdeskWritePermit, call sdkgo.Call, client *Client, response freshdeskResponse, failure sdkgo.Failure) sdkgo.MutationAttempt[OUT] {
	providerID := acceptedProviderID(response.body)
	if providerID == 0 {
		return resendOrSelectUncertain[OUT](permit, failure, 0, client.receipt(call, response, 0))
	}
	recordAcceptedWrite(permit, providerID)
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, failure, client.receipt(call, response, providerID))
}

// acceptedProviderID returns the positive top-level integer id of a 2xx body, or 0 when it has none.
func acceptedProviderID(body []byte) int64 {
	// A map keeps the key match exact, as the Python and TypeScript ports read it.
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return 0
	}
	var providerID int64
	if json.Unmarshal(fields["id"], &providerID) != nil || providerID < 1 {
		return 0
	}
	return providerID
}

// recordAcceptedWrite checkpoints the accepted provider ID; acceptance is already known if it fails.
func recordAcceptedWrite(permit freshdeskWritePermit, providerID int64) {
	_ = permit.fence.RecordProviderID(strconv.FormatInt(providerID, 10))
}

// markerProviderID returns a checkpointed Freshdesk ID for the Receipt, or 0 when none is valid.
func markerProviderID(marker singledispatch.Marker) int64 {
	providerID, err := strconv.ParseInt(marker.ProviderID, 10, 64)
	if err != nil || providerID < 1 {
		return 0
	}
	return providerID
}
