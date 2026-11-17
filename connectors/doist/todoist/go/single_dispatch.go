// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// fencedCreate is one createTask attempt behind the single-dispatch Fence; unknown outcomes resend until the cap.
type fencedCreate struct {
	fence *singledispatch.Fence
	// decision is the Fence's checkpoint decision for this attempt.
	decision singledispatch.Decision
	// canResendUnknownOutcome is true while another send stays within the cap.
	canResendUnknownOutcome bool
	// hasPossibleEarlierSend is true when an earlier send of this call may have taken effect.
	hasPossibleEarlierSend bool
}

// startFencedCreate decides whether this attempt may send. It returns a terminal attempt when it may not.
func startFencedCreate(call sdkgo.Call) (*fencedCreate, sdkgo.MutationAttempt[Task], bool) {
	fence, err := singledispatch.NewFence(call, CreateTaskDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return nil, sdkgo.NewMutationRetry[Task](*failure("createTask", sdkgo.FailureAvailability, "Todoist dispatch checkpoint is unavailable, so nothing was sent in this attempt"), 0), true
	}
	decision := fence.Decide()
	switch {
	case decision.Kind == singledispatch.FailClosed:
		return nil, sdkgo.NewMutationRetry[Task](*failure("createTask", sdkgo.FailureAvailability, "Todoist dispatch checkpoint is unreadable, so nothing was sent in this attempt"), 0), true
	case decision.Marker.ProviderID == "" && decision.Kind == singledispatch.ReadBackRequired:
		return nil, sdkgo.NewMutationUncertain(Task{}, *failure("createTask", sdkgo.FailureTransport,
			"earlier attempts of this Step may have created the task and the send cap is reached, so it is not sent again"), sdkgo.Receipt{}), true
	}
	return &fencedCreate{
		fence: fence, decision: decision,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		hasPossibleEarlierSend:  decision.MaximumPriorSends > 0,
	}, sdkgo.MutationAttempt[Task]{}, false
}

// acceptedTaskID is the task ID an earlier attempt saved, or empty when none was accepted.
func (write *fencedCreate) acceptedTaskID() string { return write.decision.Marker.ProviderID }

// credentialFailureAttempt maps a credential failure before the send.
func (write *fencedCreate) credentialFailureAttempt(result exchangeResult) sdkgo.MutationAttempt[Task] {
	// notSent after a possible earlier send would hide it, so only a first-send decision records it.
	if write.decision.Kind == singledispatch.SendAllowed {
		if err := write.fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[Task](*failure("createTask", sdkgo.FailureAvailability, "Todoist credentials failed and the not-sent checkpoint could not be recorded"), 0)
		}
	}
	if result.isLocalDefect {
		return write.defectOrSelectUncertain(*result.failure)
	}
	return sdkgo.NewMutationRetry[Task](*result.failure, result.retryAfter)
}

// dispatch sends once behind the Fence; it returns a terminal attempt when the Fence forbids sending.
func (write *fencedCreate) dispatch(send func(context.Context) exchangeResult) (exchangeResult, sdkgo.MutationAttempt[Task], bool) {
	var result exchangeResult
	err := write.fence.Dispatch(func(requestContext context.Context) error {
		result = send(requestContext)
		return nil
	})
	switch {
	case err == nil:
		return result, sdkgo.MutationAttempt[Task]{}, false
	case errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime):
		return result, sdkgo.NewMutationRetry[Task](*failure("createTask", sdkgo.FailureAvailability, "Todoist dispatch checkpoint or attempt time is unavailable, so the request was not sent"), 0), true
	default:
		return result, sdkgo.NewMutationRetry[Task](*failure("createTask", sdkgo.FailureAvailability, "Todoist dispatch was not allowed, so nothing was sent"), 0), true
	}
}

// attemptForUnacceptedCreate maps a sent create without a usable task; unknown outcomes resend below the cap.
func (write *fencedCreate) attemptForUnacceptedCreate(result exchangeResult) sdkgo.MutationAttempt[Task] {
	switch {
	case result.isLocalDefect:
		// The request was never built, so nothing was sent; a lost checkpoint only keeps the claim.
		_ = write.fence.RecordCanResend()
		return write.defectOrSelectUncertain(*result.failure)
	case result.isUndispatched || result.status == http.StatusTooManyRequests || result.status == http.StatusServiceUnavailable:
		if err := write.fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[Task](*failure("createTask", sdkgo.FailureAvailability, "Todoist retry checkpoint could not be recorded"), result.retryAfter)
		}
		return sdkgo.NewMutationRetry[Task](*result.failure, result.retryAfter)
	case isDefiniteWriteRefusalStatus(result.status):
		return write.rejectOrSelectUncertain(*result.failure)
	default:
		return write.resendOrSelectUncertain(*result.failure, result.retryAfter)
	}
}

// recordAcceptedTask saves the accepted task ID so a replayed attempt reads it back instead of resending.
func (write *fencedCreate) recordAcceptedTask(taskID string) {
	// The accepted result stands; a lost identity checkpoint only keeps the claim.
	_ = write.fence.RecordProviderID(taskID)
}

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func (write *fencedCreate) resendOrSelectUncertain(cause sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[Task] {
	if write.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[Task](cause, retryAfter)
	}
	return sdkgo.NewMutationUncertain(Task{}, cause, sdkgo.Receipt{})
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send may have written.
func (write *fencedCreate) rejectOrSelectUncertain(cause sdkgo.Failure) sdkgo.MutationAttempt[Task] {
	if write.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(Task{}, cause, sdkgo.Receipt{})
	}
	if err := write.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[Task](*failure("createTask", sdkgo.FailureAvailability, "Todoist rejection checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(CreateTaskBranchProviderRejected, Task{}, &cause, sdkgo.Receipt{})
}

// defectOrSelectUncertain selects defect only when no earlier send may have written.
func (write *fencedCreate) defectOrSelectUncertain(cause sdkgo.Failure) sdkgo.MutationAttempt[Task] {
	if write.hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(Task{}, cause, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateTaskBranchDefect, Task{}, &cause, sdkgo.Receipt{})
}

// isDefiniteWriteRefusalStatus reports a status proving Todoist refused a write; others leave it unknown.
func isDefiniteWriteRefusalStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}
