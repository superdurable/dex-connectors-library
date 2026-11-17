// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// deskWritePermit is a fence that allows this attempt to send, with its send-cap facts.
type deskWritePermit struct {
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

// deskWriteAttempt is one fenced send and its classified exchange.
type deskWriteAttempt struct {
	client    *Client
	session   *operationSession
	operation string
	request   deskRequest
	result    deskExchange
}

// beginDeskWrite decides whether this attempt may send. Zoho Desk has no ticket or comment
// read-back by Call ID, so an earlier possible send is resent while the send cap allows it.
func beginDeskWrite[OUT any](call sdkgo.Call, client *Client, operation string, executeTimeout time.Duration, defect sdkgo.BranchID) (deskWritePermit, *sdkgo.MutationAttempt[OUT]) {
	var zero OUT
	fence, err := singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		attempt := sdkgo.NewMutationBranch(defect, zero, deskFailurePointer(sdkgo.FailureLocalDefect, operation, "single-dispatch fence could not start"), sdkgo.Receipt{})
		return deskWritePermit{}, &attempt
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.SendAllowed:
	case singledispatch.ReadBackThenResendAllowed:
		// A saved provider ID proves Zoho Desk accepted the write, so it is never resent.
		if decision.Marker.ProviderID != "" {
			attempt := sdkgo.NewMutationUncertain(zero, deskFailure(sdkgo.FailureTransport, operation,
				"Zoho Desk accepted an earlier attempt's write, and its answer was lost"), client.receipt(call, deskResponse{}, decision.Marker.ProviderID))
			return deskWritePermit{}, &attempt
		}
	case singledispatch.ReadBackRequired:
		attempt := sdkgo.NewMutationUncertain(zero, deskFailure(sdkgo.FailureTransport, operation,
			"earlier attempts may have sent the request and the send cap is reached, so it is not sent again"), client.receipt(call, deskResponse{}, decision.Marker.ProviderID))
		return deskWritePermit{}, &attempt
	default:
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		attempt := sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAvailability, operation,
			"the dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
		return deskWritePermit{}, &attempt
	}
	return deskWritePermit{
		fence:                   fence,
		isFirstSend:             decision.MaximumPriorSends == 0,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
	}, nil
}

// sessionAttemptForWrite maps a session that could not start; nothing was sent in this attempt.
func sessionAttemptForWrite[OUT any](permit deskWritePermit, failure *sessionFailure, branches singleDispatchBranches) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	if !permit.isFirstSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		if failure.route == sessionRetry {
			return sdkgo.NewMutationRetry[OUT](failure.failure, 0)
		}
		return sdkgo.NewMutationUncertain(zero, deskFailure(failure.failure.Kind, failure.failure.Operation,
			"an earlier attempt may have sent the request, and the connection needs reconnection to resend it"), sdkgo.Receipt{})
	}
	if err := permit.fence.RecordNotSent(); err != nil {
		return sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAvailability, failure.failure.Operation, "Dex did not record the not-sent checkpoint"), 0)
	}
	switch failure.route {
	case sessionRetry:
		return sdkgo.NewMutationRetry[OUT](failure.failure, 0)
	case sessionRejected:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(branches.defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}

// sendDeskWrite dispatches one request under the fence; a refused dispatch returns its Retry attempt.
func sendDeskWrite[OUT any](permit deskWritePermit, client *Client, session *operationSession, operation string, request deskRequest) (deskExchange, *sdkgo.MutationAttempt[OUT]) {
	attempt := &deskWriteAttempt{client: client, session: session, operation: operation, request: request}
	if err := permit.fence.Dispatch(attempt.send); err != nil {
		// Dispatch refused before sending, so nothing reached Zoho Desk in this attempt.
		retry := sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAvailability, operation, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
		return deskExchange{}, &retry
	}
	return attempt.result, nil
}

func (attempt *deskWriteAttempt) send(requestContext context.Context) error {
	attempt.result = attempt.client.exchangeFencedWrite(requestContext, attempt.session, attempt.operation, attempt.request)
	return nil
}

// deskWriteAttemptForExchange maps a sent write. Only a definite refusal selects a refusal
// branch; any other outcome may have been applied, so it resends while the send cap allows.
func deskWriteAttemptForExchange[OUT any](permit deskWritePermit, call sdkgo.Call, client *Client, session *operationSession, result deskExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		return retryUnprocessedDeskWrite[OUT](permit, result), true
	case exchangeUnavailable:
		if result.response.statusCode == http.StatusServiceUnavailable {
			return retryUnprocessedDeskWrite[OUT](permit, result), true
		}
	case exchangeNotFound:
		branch := branches.notFound
		if branch == "" {
			branch = branches.providerRejected
		}
		return rejectDeskWrite[OUT](permit, result.failure, receipt, branch), true
	case exchangeDefect:
		if permit.isFirstSend {
			return rejectDeskWrite[OUT](permit, result.failure, sdkgo.Receipt{}, branches.defect), true
		}
	case exchangeRejected:
		if result.response.statusCode == http.StatusUnauthorized {
			return rejectedCredentialAttemptForWrite[OUT](permit, call, client, session, result, receipt, branches.providerRejected), true
		}
		if isDefiniteRefusalStatus(result.response.statusCode) {
			return rejectDeskWrite[OUT](permit, result.failure, receipt, branches.providerRejected), true
		}
	}
	return resendOrSelectUncertain[OUT](permit, result.failure, result.retryAfter, receipt), true
}

// retryUnprocessedDeskWrite retries a send Zoho Desk did not process.
func retryUnprocessedDeskWrite[OUT any](permit deskWritePermit, result deskExchange) sdkgo.MutationAttempt[OUT] {
	// A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
	_ = permit.fence.RecordCanResend()
	return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
}

// rejectedCredentialAttemptForWrite refreshes after a 401 outside the fence. A changed token retries,
// because the 401 proves this send applied nothing; otherwise the refusal gate decides.
func rejectedCredentialAttemptForWrite[OUT any](permit deskWritePermit, call sdkgo.Call, client *Client, session *operationSession, result deskExchange, receipt sdkgo.Receipt, providerRejected sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	operation := result.failure.Operation
	if _, supportsRejection := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supportsRejection {
		return rejectDeskWrite[OUT](permit, result.failure, receipt, providerRejected)
	}
	replacement, err := sdkgo.ResolveCredentialAfterRejection(session.context, client.credentials, call, client.refreshDriver)
	if err == nil && validateResolvedCredentials(replacement) == nil && replacement.AuthMethodID == session.credentials.AuthMethodID &&
		replacement.AccessToken.Reveal() != session.credentials.AccessToken.Reveal() {
		if err := permit.fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAvailability, operation, "Dex did not record the credential resend checkpoint"), 0)
		}
		return sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAuthentication, operation,
			"Zoho Desk rejected the prior access token (HTTP 401); retry with its replacement"), 0)
	}
	failure := result.failure
	if err != nil && !sdkgo.IsReauthorizationRequired(err) {
		failure.Message = "Zoho Desk rejected the access token (HTTP 401); credential refresh is temporarily unavailable"
	}
	return rejectDeskWrite[OUT](permit, failure, receipt, providerRejected)
}

// isDefiniteRefusalStatus reports a status that proves Zoho Desk applied nothing.
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

// rejectDeskWrite selects a refusal branch only when no earlier send of this call may have applied.
func rejectDeskWrite[OUT any](permit deskWritePermit, failure sdkgo.Failure, receipt sdkgo.Receipt, branch sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(zero, failure, receipt)
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAvailability, failure.Operation, "Dex did not record the refusal checkpoint"), 0)
	}
	return sdkgo.NewMutationBranch(branch, zero, &failure, receipt)
}

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func resendOrSelectUncertain[OUT any](permit deskWritePermit, failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if permit.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[OUT](failure, retryAfter)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, failure, receipt)
}

// acceptedWriteAttempt handles a 2xx body that failed to decode.
// A decoded provider ID proves acceptance, so only an ID-less answer is resent.
func acceptedWriteAttempt[OUT any](permit deskWritePermit, providerID string, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if providerID != "" {
		recordAcceptedWrite(permit, providerID)
		receipt.ProviderObjectID = providerID
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, failure, receipt)
	}
	return resendOrSelectUncertain[OUT](permit, failure, 0, receipt)
}

// recordAcceptedWrite checkpoints the accepted provider ID; acceptance is already known if it fails.
func recordAcceptedWrite(permit deskWritePermit, providerID string) {
	_ = permit.fence.RecordProviderID(providerID)
}

// acceptedProviderID returns the valid id of a 2xx object body, or "" when it has none.
func acceptedProviderID(body []byte) string {
	var answer struct {
		ID zohoID `json:"id"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	return string(answer.ID)
}
