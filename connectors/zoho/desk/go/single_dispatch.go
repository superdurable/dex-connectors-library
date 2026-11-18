// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// deskWritePermit is a fence that allows this attempt to send, or to read back an accepted write,
// with its send-cap facts.
type deskWritePermit struct {
	fence *singledispatch.Fence
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
	// canResendUnknownOutcome means this attempt's send leaves room under the cap for one more.
	canResendUnknownOutcome bool
	// savedProviderID is the accepted write an earlier attempt checkpointed; it is read back, never resent.
	savedProviderID string
}

// deskWriteReadBack reads an accepted write back by its provider ID. isProviderIDSaved reports
// that the ID is checkpointed, so a transient read failure may retry without risking a resend.
type deskWriteReadBack[OUT any] func(providerID string, isProviderIDSaved bool) sdkgo.MutationAttempt[OUT]

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

// beginDeskWrite decides whether this attempt may send. A saved provider ID is read back by ID and
// never resent. Otherwise Zoho Desk has no ticket or comment read-back by Call ID, so an earlier
// possible send is resent while the send cap allows it.
func beginDeskWrite[OUT any](call sdkgo.Call, client *Client, operation string, executeTimeout time.Duration, defect sdkgo.BranchID) (deskWritePermit, *sdkgo.MutationAttempt[OUT]) {
	var zero OUT
	fence, err := singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		attempt := sdkgo.NewMutationBranch(defect, zero, deskFailurePointer(sdkgo.FailureLocalDefect, operation, "single-dispatch fence could not start"), sdkgo.Receipt{})
		return deskWritePermit{}, &attempt
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.SendAllowed, singledispatch.ReadBackThenResendAllowed, singledispatch.ReadBackRequired:
	default:
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		attempt := sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAvailability, operation,
			"the dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
		return deskWritePermit{}, &attempt
	}
	if decision.Marker.ProviderID != "" {
		return deskWritePermit{fence: fence, savedProviderID: decision.Marker.ProviderID}, nil
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		attempt := sdkgo.NewMutationUncertain(zero, deskFailure(sdkgo.FailureTransport, operation,
			"earlier attempts may have sent the request and the send cap is reached, so it is not sent again"), client.receipt(call, deskResponse{}, ""))
		return deskWritePermit{}, &attempt
	}
	return deskWritePermit{
		fence:                   fence,
		isFirstSend:             decision.MaximumPriorSends == 0,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
	}, nil
}

// readBackAcceptedDeskWrite reads an accepted write back by its provider ID and never sends it.
// A confirmed write selects success. A transient read failure retries only when the ID is
// checkpointed, because without it the next attempt could resend; anything else is uncertain.
func readBackAcceptedDeskWrite[OUT any](call sdkgo.Call, client *Client, session *operationSession, operation string, providerID string,
	isProviderIDSaved bool, request deskRequest, success sdkgo.BranchID, decode func([]byte) (OUT, error)) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	result := client.exchange(session, operation, request)
	receipt := client.receipt(call, result.response, providerID)
	switch result.outcome {
	case exchangeSucceeded:
		value, err := decode(result.response.body)
		if err != nil {
			return sdkgo.NewMutationUncertain(zero, deskFailure(sdkgo.FailureProtocol, operation,
				"Zoho Desk accepted the write, but its read-back was invalid: "+err.Error()), receipt)
		}
		return sdkgo.NewMutationBranch(success, value, nil, receipt)
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		if isProviderIDSaved {
			return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
		}
	}
	return sdkgo.NewMutationUncertain(zero, result.failure, receipt)
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
func deskWriteAttemptForExchange[OUT any](permit deskWritePermit, call sdkgo.Call, client *Client, session *operationSession, result deskExchange, receipt sdkgo.Receipt,
	branches singleDispatchBranches, readBack deskWriteReadBack[OUT]) (sdkgo.MutationAttempt[OUT], bool) {
	if result.outcome != exchangeSucceeded && isSuccessStatus(result.response.statusCode) {
		// A 2xx accepted the write; with no readable ID it is never resent.
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt), true
	}
	if attempt, isUnconfirmedRefusal := unconfirmedRefusalAttempt(permit, result, receipt, readBack); isUnconfirmedRefusal {
		return attempt, true
	}
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
		// rejectDeskWrite selects uncertain after an earlier possible send.
		return rejectDeskWrite[OUT](permit, result.failure, sdkgo.Receipt{}, branches.defect), true
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

// unconfirmedRefusalAttempt handles a refusal status that does not prove nothing was applied.
// A body carrying a created id is read back by that id; an oversized or untrusted body is unknown.
func unconfirmedRefusalAttempt[OUT any](permit deskWritePermit, result deskExchange, receipt sdkgo.Receipt, readBack deskWriteReadBack[OUT]) (sdkgo.MutationAttempt[OUT], bool) {
	isRefusal := result.outcome == exchangeNotFound || (result.outcome == exchangeRejected && isDefiniteRefusalStatus(result.response.statusCode))
	if !isRefusal {
		return sdkgo.MutationAttempt[OUT]{}, false
	}
	status := result.response.statusCode
	if result.acceptedProviderID != "" {
		return acceptedWriteAttempt(permit, result.acceptedProviderID, deskFailure(sdkgo.FailureProtocol, result.failure.Operation,
			fmt.Sprintf("Zoho Desk answered HTTP %d with the id of a created object, so the write may have been applied", status)), receipt, readBack), true
	}
	if result.isErrorBodyUnreadable {
		return resendOrSelectUncertain[OUT](permit, deskFailure(sdkgo.FailureTransport, result.failure.Operation,
			fmt.Sprintf("Zoho Desk answered HTTP %d, but its oversized or untrusted body may hide an accepted write", status)), result.retryAfter, receipt), true
	}
	return sdkgo.MutationAttempt[OUT]{}, false
}

// retryUnprocessedDeskWrite retries a send Zoho Desk did not process.
func retryUnprocessedDeskWrite[OUT any](permit deskWritePermit, result deskExchange) sdkgo.MutationAttempt[OUT] {
	// A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
	_ = permit.fence.RecordCanResend()
	return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
}

// rejectedCredentialAttemptForWrite refreshes after a 401 outside the fence. A changed token or a
// transient refresh failure retries, because the 401 proves this send applied nothing; a terminal
// refresh failure goes through the refusal gate.
func rejectedCredentialAttemptForWrite[OUT any](permit deskWritePermit, call sdkgo.Call, client *Client, session *operationSession, result deskExchange, receipt sdkgo.Receipt, providerRejected sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	operation := result.failure.Operation
	if _, supportsRejection := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supportsRejection {
		return rejectDeskWrite[OUT](permit, result.failure, receipt, providerRejected)
	}
	replacement, err := sdkgo.ResolveCredentialAfterRejection(session.context, client.credentials, call, client.refreshDriver)
	if err == nil && validateResolvedCredentials(replacement) == nil && replacement.AuthMethodID == session.credentials.AuthMethodID &&
		replacement.AccessToken.Reveal() != session.credentials.AccessToken.Reveal() {
		return retryRejectedCredentialWrite[OUT](permit, deskFailure(sdkgo.FailureAuthentication, operation,
			"Zoho Desk rejected the prior access token (HTTP 401); retry with its replacement"), true)
	}
	if err != nil && !isTerminalCredentialError(err) {
		return retryRejectedCredentialWrite[OUT](permit, deskFailure(sdkgo.FailureAuthentication, operation,
			"Zoho Desk rejected the access token (HTTP 401); credential refresh is temporarily unavailable"), permit.isFirstSend)
	}
	return rejectDeskWrite[OUT](permit, result.failure, receipt, providerRejected)
}

// retryRejectedCredentialWrite retries a 401. A transient refresh failure after an earlier possible
// send records nothing, so the next attempt still counts this send.
func retryRejectedCredentialWrite[OUT any](permit deskWritePermit, failure sdkgo.Failure, shouldRecordCanResend bool) sdkgo.MutationAttempt[OUT] {
	if shouldRecordCanResend {
		if err := permit.fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAvailability, failure.Operation, "Dex did not record the credential resend checkpoint"), 0)
		}
	}
	return sdkgo.NewMutationRetry[OUT](failure, 0)
}

func isSuccessStatus(statusCode int) bool { return statusCode >= 200 && statusCode < 300 }

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

// acceptedWriteAttempt handles a body that shows acceptance but failed to decode; the write is
// never resent. With a provider ID it is read back in this attempt; without one it is uncertain.
func acceptedWriteAttempt[OUT any](permit deskWritePermit, providerID string, failure sdkgo.Failure, receipt sdkgo.Receipt, readBack deskWriteReadBack[OUT]) sdkgo.MutationAttempt[OUT] {
	if providerID == "" {
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, failure, receipt)
	}
	return readBack(providerID, permit.fence.RecordProviderID(providerID) == nil)
}

// recordAcceptedWrite checkpoints the accepted provider ID; acceptance is already known if it fails.
func recordAcceptedWrite(permit deskWritePermit, providerID string) {
	_ = permit.fence.RecordProviderID(providerID)
}

// acceptedProviderID returns the valid id of an object body, or "" when it has none.
func acceptedProviderID(body []byte) string {
	var answer struct {
		ID zohoID `json:"id"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	return string(answer.ID)
}
