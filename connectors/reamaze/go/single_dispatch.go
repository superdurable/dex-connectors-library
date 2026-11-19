// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// uncertainNotFoundMessage explains a reconciliation that found no trace of the earlier dispatch.
const uncertainNotFoundMessage = "an earlier attempt may have sent the request without a confirmed outcome, and Re:amaze shows no record with its key"

type fencedReamazeSend struct {
	client      *Client
	call        sdkgo.Call
	credentials Credentials
	operation   string
	request     reamazeRequest
	result      reamazeExchange
}

func (send *fencedReamazeSend) dispatch(requestContext context.Context) error {
	send.request.requestContext = requestContext
	send.request.isFencedWrite = true
	send.result = send.client.exchange(send.call, send.credentials, send.operation, send.request)
	return nil
}

func newReamazeFence(call sdkgo.Call, executeTimeout time.Duration) (*singledispatch.Fence, error) {
	return singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
}

// reamazeWritePermit is a fence that allows this attempt to send, with its send-cap facts.
type reamazeWritePermit struct {
	fence *singledispatch.Fence
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
}

func newReamazeWritePermit(fence *singledispatch.Fence, decision singledispatch.Decision) reamazeWritePermit {
	return reamazeWritePermit{fence: fence, isFirstSend: decision.MaximumPriorSends == 0}
}

// needsReadBack reports that an earlier attempt may have sent, including SendAllowed after a canResend.
func needsReadBack(decision singledispatch.Decision) bool {
	return decision.Kind != singledispatch.SendAllowed || decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != ""
}

// permitsResendAfterReadBack reports whether a read-back that proved the write absent may send again now.
func permitsResendAfterReadBack(call sdkgo.Call, decision singledispatch.Decision) bool {
	return decision.Kind != singledispatch.ReadBackRequired && decision.Marker.ProviderID == "" &&
		decision.MaximumPriorSends < call.MaxSends()
}

// reconciliation is a read-back result: a confirmed write, a transient read failure, a definite
// absence of the write, or an unconfirmed write with its failure.
type reconciliation[OUT any] struct {
	attempt       sdkgo.MutationAttempt[OUT]
	isTransient   bool
	isUnconfirmed bool
	// isAbsent means the read-back succeeded and shows no record with the Step's key.
	isAbsent bool
	failure  sdkgo.Failure
	receipt  sdkgo.Receipt
}

func confirmedReconciliation[OUT any](attempt sdkgo.MutationAttempt[OUT]) reconciliation[OUT] {
	return reconciliation[OUT]{attempt: attempt}
}

func transientReconciliation[OUT any](result reamazeExchange, receipt sdkgo.Receipt) reconciliation[OUT] {
	return reconciliation[OUT]{attempt: sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), isTransient: true,
		failure: result.failure, receipt: receipt}
}

func unconfirmedReconciliation[OUT any](failure sdkgo.Failure, receipt sdkgo.Receipt) reconciliation[OUT] {
	return reconciliation[OUT]{isUnconfirmed: true, failure: failure, receipt: receipt}
}

func absentReconciliation[OUT any](failure sdkgo.Failure, receipt sdkgo.Receipt) reconciliation[OUT] {
	return reconciliation[OUT]{isUnconfirmed: true, isAbsent: true, failure: failure, receipt: receipt}
}

// uncertainWhenUnconfirmed selects uncertain for a read-back that forbids resending; a transient read retries.
func (result reconciliation[OUT]) uncertainWhenUnconfirmed(value OUT) sdkgo.MutationAttempt[OUT] {
	if result.isUnconfirmed {
		return sdkgo.NewMutationUncertain(value, result.failure, result.receipt)
	}
	return result.attempt
}

// acceptedWriteAttempt maps the read-back of a write Re:amaze accepted with a 2xx, which is never resent.
// A transient read-back retries only when a saved ProviderID forces every later attempt to read back.
func (result reconciliation[OUT]) acceptedWriteAttempt(value OUT, isProviderIDSaved bool) sdkgo.MutationAttempt[OUT] {
	if result.isUnconfirmed || (result.isTransient && !isProviderIDSaved) {
		return sdkgo.NewMutationUncertain(value, result.failure, result.receipt)
	}
	return result.attempt
}

// fencedCredentialAttempt maps a credential failure before dispatch. After a possible earlier send it
// records nothing: a transient failure retries and unusable credentials select uncertain.
func fencedCredentialAttempt[OUT any](client *Client, call sdkgo.Call, fence *singledispatch.Fence, decision singledispatch.Decision,
	operation string, objectID string, defect sdkgo.BranchID, err error,
) sdkgo.MutationAttempt[OUT] {
	if needsReadBack(decision) {
		return uncertainOrRetryCredentials[OUT](client, call, operation, objectID, err)
	}
	if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
		return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation,
			errors.Join(err, checkpointErr).Error()), 0)
	}
	if !errors.Is(err, errReamazeCredentialsUnavailable) {
		var zero OUT
		return sdkgo.NewMutationBranch(defect, zero, reamazeFailurePointer(sdkgo.FailureAuthentication, operation,
			"Re:amaze connection credentials are unusable; reconnect the connection in Dex Web"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation, err.Error()), 0)
}

func uncertainOrRetryCredentials[OUT any](client *Client, call sdkgo.Call, operation string, providerID string, err error) sdkgo.MutationAttempt[OUT] {
	if errors.Is(err, errReamazeCredentialsUnavailable) {
		return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation,
			"connection credentials could not be loaded to read back an earlier write"), 0)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, reamazeFailure(sdkgo.FailureAuthentication, operation,
		"the earlier write cannot be read back; reconnect the connection in Dex Web"), client.receipt(call, reamazeResponse{}, providerID))
}

func fencedDispatchError[OUT any](operation string, _ error) sdkgo.MutationAttempt[OUT] {
	// Dispatch refused before sending, so nothing reached Re:amaze in this attempt.
	return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation, "write was not sent before the attempt deadline or checkpoint"), 0)
}

type fencedWriteBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// fencedWriteResult maps a sent write. Only a definite refusal selects a refusal branch; any other
// outcome returns Retry, and the next attempt reads back before it resends or selects uncertain at the cap.
func fencedWriteResult[OUT any](permit reamazeWritePermit, result reamazeExchange, receipt sdkgo.Receipt, branches fencedWriteBranches) (sdkgo.MutationAttempt[OUT], bool) {
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
		}
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeDefect:
		// The request could not be built; after a possible earlier send this selects uncertain.
		return rejectFencedWrite[OUT](permit, result, receipt, branches.defect), true
	}
	if result.isDefiniteRefusal() {
		branch := branches.providerRejected
		if result.outcome == exchangeNotFound && branches.notFound != "" {
			branch = branches.notFound
		}
		return rejectFencedWrite[OUT](permit, result, receipt, branch), true
	}
	// A 3xx, 409, unlisted status, or refusal with an unread oversized body may follow an applied write.
	return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
}

// isDefiniteRefusal reports a listed refusal status whose body was read whole, which proves nothing applied.
func (result reamazeExchange) isDefiniteRefusal() bool {
	return (result.outcome == exchangeNotFound || result.outcome == exchangeRejected) &&
		isDefiniteRefusalStatus(result.response.statusCode) && !result.response.hasOversizedBody
}

// rejectFencedWrite selects a refusal branch only when no earlier send of this call may have applied.
func rejectFencedWrite[OUT any](permit reamazeWritePermit, result reamazeExchange, receipt sdkgo.Receipt, branch sdkgo.BranchID) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt)
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, result.failure.Operation, "refusal checkpoint could not be recorded"), 0)
	}
	if branch == sdkgo.DefectBranchID {
		receipt = sdkgo.Receipt{}
	}
	return sdkgo.NewMutationBranch(branch, zero, &result.failure, receipt)
}

// isDefiniteRefusalStatus reports a status that proves Re:amaze applied nothing.
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

func fencedReconciliationRead[OUT any](result reamazeExchange, receipt sdkgo.Receipt, operation string) (reconciliation[OUT], bool) {
	switch {
	case result.outcome == exchangeSucceeded:
		return reconciliation[OUT]{}, false
	case result.isRetryableRead():
		return transientReconciliation[OUT](result, receipt), true
	default:
		// A 404 or refusal of the read-back proves nothing about the write, so it is unconfirmed.
		return unconfirmedReconciliation[OUT](reamazeFailure(result.failure.Kind, operation,
			"an earlier attempt may have sent the request, and it could not be read back: "+result.failure.Message), receipt), true
	}
}

// dispatchKey is the Re:amaze-visible identity of one Step execution's write, derived from its idempotency key.
func dispatchKey(call sdkgo.Call) string {
	key := string(call.IdempotencyKey)
	if key == "" {
		key = string(call.ID)
	}
	return "dex-" + key
}

func hasUnusableAcceptedWrite(result reamazeExchange) bool {
	return result.response.statusCode >= http.StatusOK && result.response.statusCode < http.StatusMultipleChoices && result.outcome != exchangeSucceeded
}

func reconciliationContext(call sdkgo.Call) (context.Context, context.CancelFunc) {
	deadline, hasDeadline := call.Context.Deadline()
	if !hasDeadline {
		return context.WithCancel(call.Context)
	}
	return context.WithDeadline(call.Context, deadline.Add(-time.Second))
}
