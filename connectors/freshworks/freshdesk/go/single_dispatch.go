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

// freshdeskWritePermit is a fence that allows this attempt to send or read back, with its send-cap facts.
type freshdeskWritePermit struct {
	fence *singledispatch.Fence
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
	// canResendUnknownOutcome means this attempt's send leaves room under the cap for one more.
	canResendUnknownOutcome bool
	// savedProviderID is the ID an earlier attempt checkpointed; nonzero means read back and never send.
	savedProviderID int64
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// acceptedWriteReader decodes one operation's accepted answer and reads the write back by provider ID.
type acceptedWriteReader[OUT any] interface {
	// decodeAcceptedWrite returns the output and provider ID of a 2xx write answer.
	decodeAcceptedWrite(body []byte) (OUT, int64, error)
	// readAcceptedWrite reads the accepted write back by the provider ID Freshdesk returned.
	readAcceptedWrite(call sdkgo.Call, credentials Credentials, providerID int64) acceptedWriteReadBack[OUT]
}

// acceptedWriteReadBack is the result of reading an accepted write back by provider ID.
type acceptedWriteReadBack[OUT any] struct {
	output      OUT
	isConfirmed bool
	// result is the last read; its failure explains an unconfirmed read-back.
	result freshdeskExchange
}

// beginFreshdeskWrite decides whether this attempt may send. Freshdesk has no note or ticket
// read-back by Call ID, so an earlier possible send without a saved provider ID is resent while
// the send cap allows it; a saved provider ID is read back instead and never resent.
func beginFreshdeskWrite[OUT any](call sdkgo.Call, client *Client, operation string, executeTimeout time.Duration, defect sdkgo.BranchID) (freshdeskWritePermit, *sdkgo.MutationAttempt[OUT]) {
	var zero OUT
	fence, err := singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		attempt := sdkgo.NewMutationBranch(defect, zero, freshdeskFailurePointer(sdkgo.FailureLocalDefect, operation, "single-dispatch fence could not start"), sdkgo.Receipt{})
		return freshdeskWritePermit{}, &attempt
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		attempt := sdkgo.NewMutationRetry[OUT](freshdeskFailure(sdkgo.FailureAvailability, operation,
			"the single-dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
		return freshdeskWritePermit{}, &attempt
	}
	if decision.Marker.ProviderID != "" {
		// A saved provider ID proves Freshdesk accepted the write, so it is read back and never resent.
		savedProviderID := markerProviderID(decision.Marker)
		if savedProviderID == 0 {
			attempt := sdkgo.NewMutationUncertain(zero, freshdeskFailure(sdkgo.FailureProtocol, operation,
				"Freshdesk accepted an earlier attempt's write, and its checkpointed ID is not a Freshdesk ID"), client.receipt(call, freshdeskResponse{}, 0))
			return freshdeskWritePermit{}, &attempt
		}
		return freshdeskWritePermit{fence: fence, savedProviderID: savedProviderID}, nil
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		attempt := sdkgo.NewMutationUncertain(zero, freshdeskFailure(sdkgo.FailureTransport, operation,
			"earlier attempts may have sent the request and the send cap is reached, so it is not sent again"), client.receipt(call, freshdeskResponse{}, 0))
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

// finishFreshdeskWrite maps a sent write. Any 2xx, or an answer carrying a created ID, was accepted
// and is never resent; only a definite refusal selects a refusal branch; any other outcome may have
// been applied, so it resends while the send cap allows.
func finishFreshdeskWrite[OUT any](permit freshdeskWritePermit, call sdkgo.Call, client *Client, credentials Credentials, operation string,
	result freshdeskExchange, successBranch sdkgo.BranchID, branches singleDispatchBranches, reader acceptedWriteReader[OUT]) sdkgo.MutationAttempt[OUT] {
	if isSuccessStatus(result.response.statusCode) {
		failure := result.failure
		if result.outcome == exchangeSucceeded {
			output, providerID, err := reader.decodeAcceptedWrite(result.response.body)
			if err == nil {
				_ = permit.fence.RecordProviderID(strconv.FormatInt(providerID, 10))
				return sdkgo.NewMutationBranch(successBranch, output, nil, client.receipt(call, result.response, providerID))
			}
			failure = freshdeskFailure(sdkgo.FailureProtocol, operation, "Freshdesk accepted the write but returned an invalid answer: "+err.Error())
		}
		return completeUnreadableAcceptedWrite(permit, call, client, credentials, result.response, acceptedProviderID(result.response.body), failure, successBranch, reader)
	}
	if result.bodyProviderID > 0 {
		return completeUnreadableAcceptedWrite(permit, call, client, credentials, result.response, result.bodyProviderID, result.failure, successBranch, reader)
	}
	return freshdeskWriteAttemptForExchange[OUT](permit, result, client.receipt(call, result.response, 0), branches)
}

// completeUnreadableAcceptedWrite reads back an accepted write whose answer could not be used.
// Freshdesk has no read-back without the ID, so an answer without one selects uncertain.
func completeUnreadableAcceptedWrite[OUT any](permit freshdeskWritePermit, call sdkgo.Call, client *Client, credentials Credentials,
	response freshdeskResponse, providerID int64, failure sdkgo.Failure, successBranch sdkgo.BranchID, reader acceptedWriteReader[OUT]) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	if providerID == 0 {
		return sdkgo.NewMutationUncertain(zero, failure, client.receipt(call, response, 0))
	}
	// Without a saved ID a later attempt could resend, so a failed checkpoint forbids Retry.
	isProviderIDRecorded := permit.fence.RecordProviderID(strconv.FormatInt(providerID, 10)) == nil
	return confirmAcceptedWrite(call, client, credentials, providerID, isProviderIDRecorded, successBranch, reader)
}

// confirmAcceptedWrite reads an accepted write back by provider ID: confirmed selects success, a
// transient read failure retries only when the ID is checkpointed, and anything else selects uncertain.
func confirmAcceptedWrite[OUT any](call sdkgo.Call, client *Client, credentials Credentials, providerID int64,
	isProviderIDRecorded bool, successBranch sdkgo.BranchID, reader acceptedWriteReader[OUT]) sdkgo.MutationAttempt[OUT] {
	readBack := reader.readAcceptedWrite(call, credentials, providerID)
	receipt := client.receipt(call, readBack.result.response, providerID)
	if readBack.isConfirmed {
		return sdkgo.NewMutationBranch(successBranch, readBack.output, nil, receipt)
	}
	if isProviderIDRecorded && isTransientExchangeOutcome(readBack.result.outcome) {
		return sdkgo.NewMutationRetry[OUT](readBack.result.failure, readBack.result.retryAfter)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, readBack.result.failure, receipt)
}

// freshdeskWriteAttemptForExchange maps a sent write that Freshdesk did not accept. Only a definite
// refusal with a complete body selects a refusal branch; any other outcome resends while the cap allows.
func freshdeskWriteAttemptForExchange[OUT any](permit freshdeskWritePermit, result freshdeskExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) sdkgo.MutationAttempt[OUT] {
	switch result.outcome {
	case exchangeRateLimited, exchangeNotSent, exchangeServiceUnavailable:
		// A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
		_ = permit.fence.RecordCanResend()
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
	case exchangeNotFound:
		if result.isBodyIncomplete {
			break
		}
		branch := branches.notFound
		if branch == "" {
			branch = branches.providerRejected
		}
		return rejectFreshdeskWrite[OUT](permit, result, receipt, branch)
	case exchangeDefect:
		// defect would hide an earlier possible send, so only a first send selects it.
		if permit.isFirstSend {
			return rejectFreshdeskWrite[OUT](permit, result, sdkgo.Receipt{}, branches.defect)
		}
		// The request could not be built, so a resend would fail the same way.
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt)
	case exchangeRejected:
		if isDefiniteRefusalStatus(result.response.statusCode) && !result.isBodyIncomplete {
			return rejectFreshdeskWrite[OUT](permit, result, receipt, branches.providerRejected)
		}
	}
	return resendOrSelectUncertain[OUT](permit, result.failure, result.retryAfter, receipt)
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

// isTransientExchangeOutcome reports a read that a later attempt may repeat with a different answer.
func isTransientExchangeOutcome(outcome exchangeOutcome) bool {
	switch outcome {
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable, exchangeServiceUnavailable:
		return true
	default:
		return false
	}
}

func isSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
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

// acceptedProviderID returns the positive top-level integer id of a response body, or 0 when it has none.
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

// markerProviderID returns a checkpointed Freshdesk ID, or 0 when none is valid.
func markerProviderID(marker singledispatch.Marker) int64 {
	providerID, err := strconv.ParseInt(marker.ProviderID, 10, 64)
	if err != nil || providerID < 1 {
		return 0
	}
	return providerID
}
