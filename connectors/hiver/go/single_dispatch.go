// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"context"
	"errors"
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

func beginSingleDispatch[OUT any](call sdkgo.Call, client *Client, operation string, defectBranch sdkgo.BranchID) (*singledispatch.Fence, time.Time, *sdkgo.MutationAttempt[OUT]) {
	startedAt := time.Now()
	budget := singleDispatchExecuteTimeout
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		budget = min(budget, max(deadline.Sub(startedAt), 0))
	}
	// No read-back exists; 5/6 leaves time to classify a response after the request ends.
	fence, err := singledispatch.NewFence(call, singleDispatchExecuteTimeout, singledispatch.WithWriteWindow(5.0/6.0))
	if err != nil {
		var zero OUT
		attempt := sdkgo.NewMutationBranch(defectBranch, zero,
			hiverFailurePointer(sdkgo.FailureLocalDefect, operation, "single-dispatch fence could not start"), sdkgo.Receipt{})
		return nil, time.Time{}, &attempt
	}
	switch decision := fence.Decide(); decision.Kind {
	case singledispatch.SendAllowed:
		// Half the initial budget ends before the fence's write window minus minimum send time.
		return fence, startedAt.Add(budget / 2), nil
	case singledispatch.ReadBackRequired, singledispatch.FailClosed:
		var zero OUT
		attempt := sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureTransport, operation,
			"an earlier attempt may have sent the request, so it is not sent again"), client.receipt(call, hiverResponse{}, ""))
		return nil, time.Time{}, &attempt
	default:
		var zero OUT
		attempt := sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureLocalDefect, operation,
			"single-dispatch decision is invalid"), client.receipt(call, hiverResponse{}, ""))
		return nil, time.Time{}, &attempt
	}
}

func retryBeforeSingleDispatch[OUT any](fence *singledispatch.Fence, failure sdkgo.Failure) sdkgo.MutationAttempt[OUT] {
	if err := fence.RecordNotSent(); err != nil {
		failure = hiverFailure(sdkgo.FailureAvailability, failure.Operation, "Dex did not record the not-sent checkpoint")
	}
	return sdkgo.NewMutationRetry[OUT](failure, 0)
}

func sendOnce[OUT any](call sdkgo.Call, client *Client, credentials Credentials, operation string, request hiverRequest,
	branches singleDispatchBranches, fence *singledispatch.Fence, pacerDeadline time.Time) (hiverExchange, *sdkgo.MutationAttempt[OUT]) {
	waitContext, cancel := context.WithDeadline(call.Context, pacerDeadline)
	result, isAvailable := client.awaitRequestSlotWithContext(waitContext, operation)
	cancel()
	if !isAvailable {
		attempt := retryBeforeSingleDispatch[OUT](fence, result.failure)
		return result, &attempt
	}
	request.correlationCallID = call.ID
	sendAttempt := &hiverSendAttempt{client: client, credentials: credentials, operation: operation, request: request}
	if err := fence.Dispatch(sendAttempt.send); err != nil {
		failure := hiverFailure(sdkgo.FailureAvailability, operation, "Hiver write was not dispatched")
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			attempt := sdkgo.NewMutationRetry[OUT](failure, 0)
			return hiverExchange{}, &attempt
		}
		var zero OUT
		attempt := sdkgo.NewMutationUncertain(zero, failure, client.receipt(call, hiverResponse{}, ""))
		return hiverExchange{}, &attempt
	}
	result = sendAttempt.result
	if attempt, isTerminal := singleDispatchAttemptForExchange[OUT](result, client.receipt(call, result.response, ""), branches, fence); isTerminal {
		return result, &attempt
	}
	return result, nil
}

func singleDispatchAttemptForExchange[OUT any](result hiverExchange, receipt sdkgo.Receipt,
	branches singleDispatchBranches, fence *singledispatch.Fence) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		return retryAfterSingleDispatch[OUT](result, receipt, fence), true
	case exchangeUnavailable, exchangeInvalid:
		if result.response.statusCode == http.StatusServiceUnavailable {
			return retryAfterSingleDispatch[OUT](result, receipt, fence), true
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

func retryAfterSingleDispatch[OUT any](result hiverExchange, receipt sdkgo.Receipt, fence *singledispatch.Fence) sdkgo.MutationAttempt[OUT] {
	if err := fence.RecordCanResend(); err != nil {
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, hiverFailure(sdkgo.FailureAvailability, result.failure.Operation,
			"Dex did not record the safe retry checkpoint"), receipt)
	}
	return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
}
