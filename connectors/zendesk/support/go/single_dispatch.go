// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"context"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ticketWritePermit is a fence decision of updateTicket with its send-cap facts.
type ticketWritePermit struct {
	fence *singledispatch.Fence
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
	// isAtSendCap means earlier sends reached the cap, so this attempt only reads back.
	isAtSendCap bool
	// canResendUnknownOutcome means this attempt's send leaves room under the cap for one more.
	canResendUnknownOutcome bool
}

func newTicketWritePermit(call sdkgo.Call, fence *singledispatch.Fence, decision singledispatch.Decision) ticketWritePermit {
	return ticketWritePermit{
		fence:                   fence,
		isFirstSend:             decision.MaximumPriorSends == 0,
		isAtSendCap:             decision.Kind == singledispatch.ReadBackRequired,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
	}
}

// fencedTicketWrite sends one PUT inside the fence's write window.
type fencedTicketWrite struct {
	client      *Client
	call        sdkgo.Call
	credentials Credentials
	request     zendeskRequest
	result      zendeskExchange
}

func (write *fencedTicketWrite) dispatch(requestContext context.Context) error {
	write.request.requestContext = requestContext
	write.request.isFencedWrite = true
	write.result = write.client.exchange(write.call, write.credentials, updateTicketOperation, write.request)
	return nil
}

// retryBeforeDispatch records notSent only on a first send; otherwise the claim stays counted.
func (permit ticketWritePermit) retryBeforeDispatch(failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if permit.isFirstSend {
		if err := permit.fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[UpdateTicketOutput](zendeskFailure(sdkgo.FailureAvailability, updateTicketOperation, "Dex did not record the not-sent checkpoint"), retryAfter)
		}
	}
	return sdkgo.NewMutationRetry[UpdateTicketOutput](failure, retryAfter)
}

// unconfirmedRead retries an inconclusive read after a possible earlier send, or selects uncertain at the cap.
func (permit ticketWritePermit) unconfirmedRead(value UpdateTicketOutput, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if permit.isAtSendCap {
		return sdkgo.NewMutationUncertain(value, failure, receipt)
	}
	return sdkgo.NewMutationRetry[UpdateTicketOutput](failure, 0)
}

// readAttempt maps a ticket or audit read before the write; a definite answer after a possible send is uncertain.
func (permit ticketWritePermit) readAttempt(result zendeskExchange, receipt sdkgo.Receipt, value UpdateTicketOutput) (sdkgo.MutationAttempt[UpdateTicketOutput], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateTicketOutput]{}, false
	case exchangeRetryable, exchangeNotSent:
		return permit.retryBeforeDispatch(result.failure, result.retryAfter), true
	case exchangeInvalid, exchangeDefect:
		if !permit.isFirstSend {
			return permit.unconfirmedRead(value, result.failure, receipt), true
		}
	default:
		if !permit.isFirstSend {
			return sdkgo.NewMutationUncertain(value, result.failure, receipt), true
		}
	}
	return updateTicketBranchForExchange(result, receipt, value), true
}

// writeAttempt maps the PUT. Only a definite refusal of a first send selects a refusal branch.
func (permit ticketWritePermit) writeAttempt(result zendeskExchange, receipt sdkgo.Receipt, value UpdateTicketOutput) (sdkgo.MutationAttempt[UpdateTicketOutput], bool) {
	status := result.response.statusCode
	switch {
	case result.outcome == exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateTicketOutput]{}, false
	case result.outcome == exchangeNotSent || status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
		// A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
		_ = permit.fence.RecordCanResend()
		return sdkgo.NewMutationRetry[UpdateTicketOutput](result.failure, result.retryAfter), true
	case result.outcome == exchangeDefect && permit.isFirstSend,
		(result.outcome == exchangeRejected || result.outcome == exchangeNotFound) && isDefiniteRefusalStatus(status):
		return permit.rejectWrite(result, receipt, value), true
	}
	// A 3xx, 408, 409, 410, other 5xx, unlisted status, lost answer, or unusable 2xx may follow an applied write.
	return permit.resendOrSelectUncertain(value, result.failure, result.retryAfter, receipt), true
}

// rejectWrite selects a refusal branch only when no earlier send of this call may have applied.
func (permit ticketWritePermit) rejectWrite(result zendeskExchange, receipt sdkgo.Receipt, value UpdateTicketOutput) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(value, result.failure, receipt)
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[UpdateTicketOutput](zendeskFailure(sdkgo.FailureAvailability, updateTicketOperation, "Dex did not record the refusal checkpoint"), 0)
	}
	return updateTicketBranchForExchange(result, receipt, value)
}

// resendOrSelectUncertain retries an unknown write outcome while the send cap allows another send.
func (permit ticketWritePermit) resendOrSelectUncertain(value UpdateTicketOutput, failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if permit.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt reads back before it resends.
		return sdkgo.NewMutationRetry[UpdateTicketOutput](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(value, failure, receipt)
}

// isDefiniteRefusalStatus reports a status that proves Zendesk applied nothing.
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
