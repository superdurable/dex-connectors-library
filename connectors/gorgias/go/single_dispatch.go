// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	// reconcileDelay lets a request Gorgias may still be processing finish before the next attempt looks for it.
	reconcileDelay = 2 * time.Second
	// reconcileLookupRetryDelay spaces repeated lookups, so a short outage or a slowly visible write is still found.
	reconcileLookupRetryDelay = 15 * time.Second
	// maximumReconcileDelay caps a Retry-After on a lookup inside the five-minute retry window.
	maximumReconcileDelay = 60 * time.Second
)

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// gorgiasWritePermit is a fence decision that lets this attempt read back and send, with its send-cap facts.
type gorgiasWritePermit struct {
	fence    *singledispatch.Fence
	decision singledispatch.Decision
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
}

// beginGorgiasWrite starts the SDK fence under the manifest send cap; a returned attempt ends this attempt before any request.
func beginGorgiasWrite[OUT any](call sdkgo.Call, operation string, executeTimeout time.Duration, defect sdkgo.BranchID) (gorgiasWritePermit, *sdkgo.MutationAttempt[OUT]) {
	var zero OUT
	fence, err := singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		attempt := sdkgo.NewMutationBranch(defect, zero, gorgiasFailurePointer(sdkgo.FailureLocalDefect, operation, "single-dispatch fence could not start"), sdkgo.Receipt{})
		return gorgiasWritePermit{}, &attempt
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		attempt := sdkgo.NewMutationRetry[OUT](gorgiasFailure(sdkgo.FailureAvailability, operation,
			"the dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
		return gorgiasWritePermit{}, &attempt
	}
	return gorgiasWritePermit{fence: fence, decision: decision, isFirstSend: decision.MaximumPriorSends == 0}, nil
}

// needsReadBack reports that an earlier attempt may have sent, so Gorgias is searched by external ID first.
func (permit gorgiasWritePermit) needsReadBack() bool {
	return permit.decision.Kind != singledispatch.SendAllowed || permit.decision.MaximumPriorSends > 0
}

// canSendAfterUnconfirmedReadBack reports room under the cap and no saved ID proving Gorgias accepted a send.
func (permit gorgiasWritePermit) canSendAfterUnconfirmedReadBack() bool {
	return permit.decision.Kind != singledispatch.ReadBackRequired && permit.decision.Marker.ProviderID == ""
}

// credentialFailureAttempt maps unusable credentials before any send in this attempt.
func credentialFailureAttempt[OUT any](permit gorgiasWritePermit, failure sdkgo.Failure, defect sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	if !permit.isFirstSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		return sdkgo.NewMutationUncertain(zero, gorgiasFailure(failure.Kind, failure.Operation,
			"an earlier attempt of this Step may have sent the request and the connection credentials are unavailable to read it back"), sdkgo.Receipt{})
	}
	if err := permit.fence.RecordNotSent(); err != nil {
		return sdkgo.NewMutationRetry[OUT](gorgiasFailure(sdkgo.FailureAvailability, failure.Operation, "Dex did not record the not-sent checkpoint"), 0)
	}
	return sdkgo.NewMutationBranch(defect, zero, &failure, sdkgo.Receipt{})
}

// readBackAttempt maps an external-ID lookup; it returns false when this attempt may send.
func readBackAttempt[OUT any](permit gorgiasWritePermit, result gorgiasExchange, isFound bool, found sdkgo.MutationAttempt[OUT],
	receipt sdkgo.Receipt, operation string, resource string) (sdkgo.MutationAttempt[OUT], bool) {
	switch {
	case result.outcome == exchangeSucceeded && isFound:
		return found, true
	case result.outcome == exchangeRateLimited || result.outcome == exchangeNotSent || result.outcome == exchangeUnavailable:
		return sdkgo.NewMutationRetry[OUT](result.failure, reconcileDelayAfter(result.retryAfter, reconcileLookupRetryDelay)), true
	}
	if permit.canSendAfterUnconfirmedReadBack() {
		return sdkgo.MutationAttempt[OUT]{}, false
	}
	failure := gorgiasFailure(sdkgo.FailureTransport, operation,
		"earlier attempts of this Step may have sent the request, the send cap is reached, and Gorgias shows no "+resource+" with its external ID")
	if result.outcome != exchangeSucceeded {
		failure = gorgiasFailure(result.failure.Kind, operation,
			"earlier attempts of this Step may have sent the request, the send cap is reached, and Gorgias could not confirm it: "+result.failure.Message)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, failure, receipt), true
}

// preSendRetryAttempt retries a failed read before this attempt's send; only a first send records notSent.
func preSendRetryAttempt[OUT any](permit gorgiasWritePermit, failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[OUT] {
	if permit.decision.Kind == singledispatch.SendAllowed {
		// A lost notSent leaves no marker, so the next attempt reads back before it sends.
		_ = permit.fence.RecordNotSent()
	}
	return sdkgo.NewMutationRetry[OUT](failure, retryAfter)
}

// sendGorgiasWrite dispatches one request under the fence; a refused dispatch returns its Retry attempt.
func sendGorgiasWrite[OUT any](permit gorgiasWritePermit, client *Client, credentials Credentials, operation string, request gorgiasRequest) (gorgiasExchange, *sdkgo.MutationAttempt[OUT]) {
	var result gorgiasExchange
	if err := permit.fence.Dispatch(func(requestContext context.Context) error {
		result = client.exchangeWithContext(requestContext, credentials, operation, request)
		return nil
	}); err != nil {
		// Dispatch refused before sending, so nothing reached Gorgias in this attempt.
		attempt := sdkgo.NewMutationRetry[OUT](gorgiasFailure(sdkgo.FailureAvailability, operation,
			"Dex did not record the dispatch checkpoint or too little attempt time remained; nothing was sent"), 0)
		return result, &attempt
	}
	return result, nil
}

// writeAttemptForExchange maps a sent write. Only a definite refusal selects a refusal branch; any other
// outcome returns Retry, and the next attempt reads back before it resends or selects uncertain at the cap.
func writeAttemptForExchange[OUT any](permit gorgiasWritePermit, result gorgiasExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		// A lost checkpoint leaves the claim, so the next attempt counts this send and reads back.
		_ = permit.fence.RecordCanResend()
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeUnavailable:
		if result.response.statusCode == http.StatusServiceUnavailable {
			_ = permit.fence.RecordCanResend()
			return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
		}
	case exchangeNotFound:
		branch := branches.notFound
		if branch == "" {
			branch = branches.providerRejected
		}
		return rejectGorgiasWrite[OUT](permit, result, receipt, branch), true
	case exchangeDefect:
		if permit.isFirstSend {
			return rejectGorgiasWrite[OUT](permit, result, sdkgo.Receipt{}, branches.defect), true
		}
	case exchangeRejected:
		if isDefiniteRefusalStatus(result.response.statusCode) {
			return rejectGorgiasWrite[OUT](permit, result, receipt, branches.providerRejected), true
		}
	}
	// A 3xx, 408, 409, other 5xx, unlisted status, or unusable 2xx may follow an applied write.
	return sdkgo.NewMutationRetry[OUT](result.failure, reconcileDelayAfter(result.retryAfter, reconcileDelay)), true
}

// isDefiniteRefusalStatus reports a status that proves Gorgias applied nothing.
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

// rejectGorgiasWrite selects a refusal branch only when no earlier send of this call may have applied.
func rejectGorgiasWrite[OUT any](permit gorgiasWritePermit, result gorgiasExchange, receipt sdkgo.Receipt, branch sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt)
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[OUT](gorgiasFailure(sdkgo.FailureAvailability, result.failure.Operation, "Dex did not record the refusal checkpoint"), 0)
	}
	return sdkgo.NewMutationBranch(branch, zero, &result.failure, receipt)
}

// recordAcceptedWrite checkpoints the accepted provider ID; acceptance is already known if it fails.
func recordAcceptedWrite(permit gorgiasWritePermit, providerID int64) {
	_ = permit.fence.RecordProviderID(strconv.FormatInt(providerID, 10))
}

// reconcileDelayAfter honors a Gorgias Retry-After of at least minimumDelay, capped by maximumReconcileDelay.
func reconcileDelayAfter(retryAfter time.Duration, minimumDelay time.Duration) time.Duration {
	return min(max(retryAfter, minimumDelay), maximumReconcileDelay)
}
