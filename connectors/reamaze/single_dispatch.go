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
	return singledispatch.NewFence(call, executeTimeout)
}

func fencedCredentialAttempt[OUT any](fence *singledispatch.Fence, operation string, defect sdkgo.BranchID, err error) sdkgo.MutationAttempt[OUT] {
	if !errors.Is(err, errReamazeCredentialsUnavailable) {
		var zero OUT
		return sdkgo.NewMutationBranch(defect, zero, reamazeFailurePointer(sdkgo.FailureAuthentication, operation,
			"Re:amaze connection credentials are unusable; reconnect the connection in Dex Web"), sdkgo.Receipt{})
	}
	if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
		return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation,
			errors.Join(err, checkpointErr).Error()), 0)
	}
	return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation, err.Error()), 0)
}

func uncertainOrRetryCredentials[OUT any](operation string, err error) sdkgo.MutationAttempt[OUT] {
	if errors.Is(err, errReamazeCredentialsUnavailable) {
		return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation, err.Error()), 0)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, reamazeFailure(sdkgo.FailureAuthentication, operation,
		"the earlier write cannot be read back until the connection is restored"), sdkgo.Receipt{})
}

func fencedDispatchError[OUT any](operation string, err error) sdkgo.MutationAttempt[OUT] {
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation, "write was not sent before the attempt deadline or checkpoint"), 0)
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, reamazeFailure(sdkgo.FailureTransport, operation, "write dispatch outcome could not be confirmed"), sdkgo.Receipt{})
}

type fencedWriteBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

func fencedWriteResult[OUT any](fence *singledispatch.Fence, result reamazeExchange, receipt sdkgo.Receipt, branches fencedWriteBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, result.failure.Operation, "retry checkpoint could not be recorded"), result.retryAfter), true
		}
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeUnavailable:
		if result.response.statusCode == http.StatusServiceUnavailable {
			if err := fence.RecordCanResend(); err != nil {
				return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, result.failure.Operation, "retry checkpoint could not be recorded"), result.retryAfter), true
			}
		}
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeRejected:
		if result.response.statusCode == http.StatusConflict {
			return sdkgo.NewMutationRetry[OUT](result.failure, 0), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt), true
	case exchangeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewMutationBranch(branches.notFound, zero, &result.failure, receipt), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(branches.defect, zero, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	}
}

func fencedReconciliationRead[OUT any](result reamazeExchange, receipt sdkgo.Receipt, operation string) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch {
	case result.outcome == exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	default:
		return sdkgo.NewMutationUncertain(zero, reamazeFailure(result.failure.Kind, operation,
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
