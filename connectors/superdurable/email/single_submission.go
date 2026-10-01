// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleSubmissionMarker is the Dex heartbeat checkpoint recorded before a message is submitted.
type singleSubmissionMarker struct {
	SubmittedCallID sdkgo.CallID `json:"emailSubmittedCallId"`
}

// submissionClaim is whether this attempt may submit its message.
type submissionClaim uint8

const (
	// submissionGranted means no earlier attempt submitted the message and the marker is recorded.
	submissionGranted submissionClaim = iota + 1
	// submissionAlreadyClaimed means an earlier attempt of this Step execution may have submitted it.
	submissionAlreadyClaimed
	// submissionNotRecorded means Dex did not accept the marker, so nothing may be submitted yet.
	submissionNotRecorded
)

// hasEarlierSubmissionClaim reports a marker from an earlier attempt; an unreadable checkpoint counts as one.
func hasEarlierSubmissionClaim(call sdkgo.Call) bool {
	var marker singleSubmissionMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	return err != nil || isMarkerFound
}

// claimSingleSubmission records the marker before the first submission; a later attempt that finds it must not submit.
func claimSingleSubmission(call sdkgo.Call) submissionClaim {
	if hasEarlierSubmissionClaim(call) {
		return submissionAlreadyClaimed
	}
	if err := call.Context.RecordHeartbeat(singleSubmissionMarker{SubmittedCallID: call.ID}); err != nil {
		return submissionNotRecorded
	}
	return submissionGranted
}

// releaseSingleSubmission clears the marker after the server provably did not accept the message.
func releaseSingleSubmission(call sdkgo.Call) {
	// A failed clear leaves the marker, so the next attempt selects uncertain instead of submitting again.
	_ = call.Context.RecordHeartbeat(nil)
}

// singleSubmissionBranches names an operation's branches for the shared submission outcome mapping.
type singleSubmissionBranches struct {
	sent             sdkgo.BranchID
	providerRejected sdkgo.BranchID
}

// submissionAttemptBeforeSend returns the attempt for a claim that does not permit submitting.
func submissionAttemptBeforeSend(claim submissionClaim, operation string, message SentMessage) (sdkgo.MutationAttempt[SentMessage], bool) {
	switch claim {
	case submissionGranted:
		return sdkgo.MutationAttempt[SentMessage]{}, false
	case submissionNotRecorded:
		return sdkgo.NewMutationRetry[SentMessage](emailFailure(sdkgo.FailureAvailability, operation,
			"Dex did not record the submission checkpoint; nothing was submitted"), 0), true
	default:
		// This attempt did not submit, so the earlier attempt's Date header is unknown.
		message.SentAt = time.Time{}
		return sdkgo.NewMutationUncertain(message, emailFailure(sdkgo.FailureTransport, operation,
			"an earlier attempt of this Step may have submitted the message, so it is not submitted again"),
			sdkgo.Receipt{ProviderObjectID: message.MessageID}), true
	}
}

// submissionAttemptForResult maps a submission: only a provable non-acceptance clears the marker.
func submissionAttemptForResult(call sdkgo.Call, result submissionResult, message SentMessage, branches singleSubmissionBranches) sdkgo.MutationAttempt[SentMessage] {
	receipt := sdkgo.Receipt{ProviderObjectID: message.MessageID}
	switch result.outcome {
	case submissionAccepted:
		return sdkgo.NewMutationBranch(branches.sent, message, nil, receipt)
	case submissionUncertain:
		return sdkgo.NewMutationUncertain(message, result.failure, receipt)
	default:
		releaseSingleSubmission(call)
		if result.isRetryable {
			return sdkgo.NewMutationRetry[SentMessage](result.failure, 0)
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, SentMessage{}, &result.failure, sdkgo.Receipt{})
	}
}
