// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// fencedWrite is one QuickBooks write attempt under a single-dispatch fence capped by call.MaxSends.
type fencedWrite[OUT any] struct {
	client    *Client
	call      sdkgo.Call
	operation string
	fence     *singledispatch.Fence
	requested OUT
	branches  mutationBranches
	readBack  acceptedWriteReadBack[OUT]
	// canResendUnknownOutcome is false when this attempt's send is the last one the cap allows.
	canResendUnknownOutcome bool
	// hasPossibleEarlierSend means a refusal of this send cannot prove nothing was written.
	hasPossibleEarlierSend bool
}

// acceptedWriteReadBack confirms an accepted write by reading its QuickBooks entity by ID.
type acceptedWriteReadBack[OUT any] struct {
	// entityPath is the read path segment, such as customer for GET /customer/{id}.
	entityPath string
	happy      sdkgo.BranchID
	// confirm decodes the read entity and reports whether it is this Step's write.
	confirm func(body []byte, entityID string) (OUT, bool)
}

// newFencedWrite decides the attempt before credentials; isDecided means the attempt needs no send.
func newFencedWrite[OUT any](client *Client, call sdkgo.Call, operation string, executeTimeout time.Duration, requested OUT,
	branches mutationBranches, readBack acceptedWriteReadBack[OUT]) (write *fencedWrite[OUT], attempt sdkgo.MutationAttempt[OUT], isDecided bool) {
	fence, err := singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return nil, sdkgo.NewMutationBranch(branches.defect, requested,
			quickbooksFailurePointer(sdkgo.FailureLocalDefect, operation, "QuickBooks dispatch fence could not be constructed"), sdkgo.Receipt{}), true
	}
	decision := fence.Decide()
	write = &fencedWrite[OUT]{
		client: client, call: call, operation: operation, fence: fence, requested: requested, branches: branches, readBack: readBack,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		hasPossibleEarlierSend:  decision.MaximumPriorSends > 0,
	}
	switch {
	case decision.Kind == singledispatch.FailClosed:
		return nil, sdkgo.NewMutationRetry[OUT](quickbooksFailure(sdkgo.FailureAvailability, operation,
			"the dispatch checkpoint could not be read; nothing was sent in this attempt"), 0), true
	case decision.Marker.ProviderID != "":
		// QuickBooks accepted an earlier send with this ID, so it is read back and never resent.
		return nil, write.readBackOnly(decision.Marker.ProviderID), true
	case decision.Kind == singledispatch.ReadBackRequired:
		return nil, sdkgo.NewMutationUncertain(requested, quickbooksFailure(sdkgo.FailureTransport, operation,
			"an earlier attempt may have written to QuickBooks and the send cap is reached"), sdkgo.Receipt{}), true
	}
	return write, sdkgo.MutationAttempt[OUT]{}, false
}

// dispatch sends request once under the fence; a non-nil attempt means nothing was sent.
func (write *fencedWrite[OUT]) dispatch(request quickbooksRequest) (quickbooksExchange, *sdkgo.MutationAttempt[OUT]) {
	var result quickbooksExchange
	err := write.fence.Dispatch(func(requestContext context.Context) error {
		result = write.client.exchangeWithContext(requestContext, write.call, write.operation, request)
		return nil
	})
	if err == nil {
		return result, nil
	}
	message := "too little of the Step deadline remained to send; nothing was sent in this attempt"
	if errors.Is(err, singledispatch.ErrCheckpoint) {
		message = "Dex did not record the dispatch checkpoint; nothing was sent in this attempt"
	}
	retry := sdkgo.NewMutationRetry[OUT](quickbooksFailure(sdkgo.FailureAvailability, write.operation, message), 0)
	return quickbooksExchange{}, &retry
}

// attemptForExchange maps every outcome except success and an accepted but unusable 2xx.
func (write *fencedWrite[OUT]) attemptForExchange(result quickbooksExchange, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[OUT], bool) {
	switch {
	case result.outcome == exchangeSucceeded, result.outcome == exchangeInvalid, result.outcome == exchangeInterrupted:
		return sdkgo.MutationAttempt[OUT]{}, false
	case result.isProvablyUnsent && result.outcome == exchangeRetry,
		result.outcome == exchangeRetry && (result.response.statusCode == http.StatusTooManyRequests || result.response.statusCode == http.StatusServiceUnavailable):
		if err := write.fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[OUT](quickbooksFailure(sdkgo.FailureAvailability, write.operation,
				"the QuickBooks retry checkpoint could not be recorded"), 0), true
		}
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case !result.isProvablyUnsent && isWriteOutcomeUnknown(result):
		return write.resendOrSelectUncertain(result.failure, result.retryAfter, receipt), true
	}
	// The refusal proves nothing was written; a lost checkpoint only makes a replay count this send.
	_ = write.fence.RecordCanResend()
	branch := write.branches.providerRejected
	switch {
	case result.outcome == exchangeDefect:
		branch = write.branches.defect
	case result.outcome == exchangeNameConflict && write.branches.nameConflict != "":
		branch = write.branches.nameConflict
	}
	if write.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(write.requested, quickbooksFailure(result.failure.Kind, write.operation,
			"an earlier attempt may have written to QuickBooks, so this refusal cannot prove nothing was written: "+result.failure.Message), receipt), true
	}
	return sdkgo.NewMutationBranch(branch, write.requested, &result.failure, receipt), true
}

// unusableAccepted reads back an accepted 2xx that failed validation and otherwise treats it as unknown.
func (write *fencedWrite[OUT]) unusableAccepted(entityID string, receipt sdkgo.Receipt, err error) sdkgo.MutationAttempt[OUT] {
	failure := quickbooksFailure(sdkgo.FailureProtocol, write.operation, "QuickBooks accepted the write but returned an unusable answer: "+err.Error())
	if entityID == "" {
		return write.resendOrSelectUncertain(failure, 0, receipt)
	}
	// A recorded ID makes every later attempt read back instead of resending.
	_ = write.fence.RecordProviderID(entityID)
	if body, isFound := readAcceptedEntity(write.client, write.call, write.operation, write.readBack.entityPath, entityID); isFound {
		if output, isConfirmed := write.readBack.confirm(body, entityID); isConfirmed {
			receipt.ProviderObjectID = entityID
			return sdkgo.NewMutationBranch(write.readBack.happy, output, nil, receipt)
		}
	}
	return write.resendOrSelectUncertain(failure, 0, receipt)
}

// readBackOnly confirms an accepted earlier write by ID; it never sends.
func (write *fencedWrite[OUT]) readBackOnly(entityID string) sdkgo.MutationAttempt[OUT] {
	result := write.client.exchange(write.call, write.operation, quickbooksRequest{method: http.MethodGet, path: "/" + write.readBack.entityPath + "/" + entityID})
	receipt := write.client.receipt(write.call, result, entityID)
	switch result.outcome {
	case exchangeRetry, exchangeInterrupted:
		// Reading is harmless to repeat, and a retry of this path never sends.
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter)
	case exchangeSucceeded:
		if output, isConfirmed := write.readBack.confirm(result.response.body, entityID); isConfirmed {
			return sdkgo.NewMutationBranch(write.readBack.happy, output, nil, receipt)
		}
	}
	return sdkgo.NewMutationUncertain(write.requested, quickbooksFailure(sdkgo.FailureProtocol, write.operation,
		"QuickBooks accepted an earlier attempt's write, but reading entity "+entityID+" back could not confirm it"), receipt)
}

// resendOrSelectUncertain retries a possibly applied write while the send cap allows another send.
func (write *fencedWrite[OUT]) resendOrSelectUncertain(failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[OUT] {
	if write.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[OUT](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(write.requested, failure, receipt)
}

// isWriteOutcomeUnknown reports an outcome after a send that does not prove QuickBooks refused the write.
func isWriteOutcomeUnknown(result quickbooksExchange) bool {
	switch result.outcome {
	case exchangeRetry:
		return true
	case exchangeRejected:
		// A Fault at 2xx or 400 is a definite business refusal; other unlisted statuses are unknown.
		return !isDefiniteRefusalStatus(result.response.statusCode) && !isSuccessStatus(result.response.statusCode)
	default:
		return false
	}
}

// isDefiniteRefusalStatus lists the statuses that prove a write was refused; 409, 501, 3xx, and others are unknown.
func isDefiniteRefusalStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}
