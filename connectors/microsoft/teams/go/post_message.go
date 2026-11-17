// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	// writeAttributionSkew tolerates clock differences between the Worker, Dex, and Microsoft.
	writeAttributionSkew = time.Minute
	// ambiguousWriteSettleDelay lets an unconfirmed post appear in the channel before the next attempt looks for it.
	ambiguousWriteSettleDelay = 5 * time.Second
	// readBackPageSize is Graph's largest page of channel messages or replies.
	readBackPageSize = 50
)

// messagePost is one validated post: where it goes, its body, and where a read-back looks for it.
type messagePost struct {
	operationID          string
	executeMethodTimeout time.Duration
	subject              string
	path                 string
	content              messageContent
	requested            PostMessageOutput
	branches             postBranches
	// readBackPath lists the newest messages the post would join; empty means it cannot be read back.
	readBackPath string
	// readBackReplyToID is the root message a reply belongs to; empty for a root channel message.
	readBackReplyToID string
}

// postBranches names one post operation's branches.
type postBranches struct {
	sent             sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// messageSendAttempt keeps the one fenced provider exchange and its result together.
type messageSendAttempt struct {
	client  *Client
	session *operationSession
	post    messagePost
	result  exchangeResult
}

func (attempt *messageSendAttempt) send(requestContext context.Context) error {
	attempt.result = attempt.client.exchangeFencedWrite(attempt.session, attempt.post, requestContext)
	return nil
}

// send resends an unknown outcome while possible sends stay below call.MaxSends(). A post that can be
// read back is looked for before each resend and at the cap; otherwise uncertain is selected at the cap.
func (client *Client) send(call sdkgo.Call, post messagePost) sdkgo.MutationAttempt[PostMessageOutput] {
	fence, err := singledispatch.NewFence(call, post.executeMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(post.branches.defect, post.requested,
			failurePointer(post.operationID, sdkgo.FailureLocalDefect, "Microsoft Teams dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[PostMessageOutput](newFailure(post.operationID, sdkgo.FailureAvailability,
			"the Microsoft Teams dispatch checkpoint is unreadable; no message was sent in this attempt"), 0)
	}
	// This attempt's send is the last one the cap allows when no further send remains.
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	// An earlier send may have posted, so a refusal or local failure now cannot prove nothing changed.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	session, cancel, failure := client.startSession(call, post.operationID)
	if failure != nil {
		credentialFailure := failure.failure
		if decision.Kind == singledispatch.SendAllowed {
			if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
				credentialFailure.Message += "; the not-sent checkpoint also failed"
			}
		}
		if !failure.isReauthorizationRequired {
			return sdkgo.NewMutationRetry[PostMessageOutput](credentialFailure, 0)
		}
		if hasPossibleEarlierSend {
			return sdkgo.NewMutationUncertain(post.requested, credentialFailure, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(post.branches.defect, post.requested, &credentialFailure, sdkgo.Receipt{})
	}
	defer cancel()
	switch decision.Kind {
	case singledispatch.ReadBackRequired:
		reconciled, _ := client.reconcileEarlierPost(session, post, call.Context.FirstAttemptAt(), decision.Marker.ProviderID, false)
		return reconciled
	case singledispatch.ReadBackThenResendAllowed:
		// A saved message ID proves Graph stored an earlier post, so it is read back and never resent.
		if post.readBackPath != "" || decision.Marker.ProviderID != "" {
			reconciled, shouldResend := client.reconcileEarlierPost(session, post, call.Context.FirstAttemptAt(), decision.Marker.ProviderID, true)
			if !shouldResend {
				return reconciled
			}
		}
	}
	attempt := &messageSendAttempt{client: client, session: session, post: post}
	if err := fence.Dispatch(attempt.send); err != nil {
		if !errors.Is(err, singledispatch.ErrCheckpoint) && !errors.Is(err, singledispatch.ErrInsufficientTime) {
			return client.resendOrSelectUncertain(post, canResendUnknownOutcome, newFailure(post.operationID, sdkgo.FailureTransport,
				"Microsoft Teams message dispatch outcome is unknown"), 0, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PostMessageOutput](newFailure(post.operationID, sdkgo.FailureAvailability,
			"the "+post.subject+" was not sent before the dispatch deadline"), 0)
	}
	result := attempt.result
	var refreshFailure error
	if result.err == nil && result.response.statusCode == http.StatusUnauthorized {
		if _, supportsRefresh := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); supportsRefresh {
			replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(session.context, client.credentials, call, client.refreshDriver)
			refreshFailure = refreshErr
			if refreshErr == nil && validateResolvedCredentials(replacement) == nil &&
				replacement.AccessToken.Reveal() != session.credentials.AccessToken.Reveal() {
				// Graph rejects unauthenticated requests before processing them; only a changed token permits one resend.
				failure := newFailure(post.operationID, sdkgo.FailureAuthentication,
					"Microsoft Graph rejected the access token; the refreshed credential will be used on the next attempt")
				if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
					failure.Message += "; the safe-resend checkpoint failed"
				}
				return sdkgo.NewMutationRetry[PostMessageOutput](failure, 0)
			}
		}
	}
	classification := client.classifyWrite(post.operationID, post.subject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeAccepted:
		var resource chatMessageResource
		if json.Unmarshal(result.response.body, &resource) == nil {
			isMessageIDSaved := false
			if messageIDPattern.MatchString(resource.ID) {
				// Acceptance is known; a lost read-back checkpoint still counts this send against the cap.
				isMessageIDSaved = fence.RecordProviderID(resource.ID) == nil
			}
			if message, err := decodeMessage(resource, 0); err == nil {
				return sdkgo.NewMutationBranch(post.branches.sent, postedOutput(post.requested, message, false), nil, client.receipt(session, result.response, message.ID))
			}
			if isMessageIDSaved && post.readBackPath == "" {
				// Graph stored this message, and a resend would duplicate it; it cannot be read back.
				return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureProtocol,
					"Microsoft Graph stored the "+post.subject+" but returned an unusable message, and the connection cannot read it back"), receipt)
			}
		}
		classification.failure = newFailure(post.operationID, sdkgo.FailureProtocol, "Microsoft Graph accepted the "+post.subject+" but returned an unusable message")
	case writeNotApplied:
		if err := fence.RecordCanResend(); err != nil {
			classification.failure.Message += "; the safe-resend checkpoint failed"
		}
		return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, classification.retryAfter)
	case writeRejected:
		var rejectedResource chatMessageResource
		if json.Unmarshal(result.response.body, &rejectedResource) == nil && messageIDPattern.MatchString(rejectedResource.ID) {
			// A message ID in a refusal contradicts it, so the outcome is unknown.
			return client.resendOrSelectUncertain(post, canResendUnknownOutcome, newFailure(post.operationID, sdkgo.FailureProtocol,
				"Microsoft Graph refused the "+post.subject+" but returned a message ID"), classification.retryAfter, receipt)
		}
		if result.response.statusCode == http.StatusUnauthorized && refreshFailure != nil && !sdkgo.IsReauthorizationRequired(refreshFailure) {
			classification.failure.Message = strings.TrimSuffix(classification.failure.Message, "; reconnect the connection") + "; credential refresh is temporarily unavailable"
		}
		if hasPossibleEarlierSend {
			classification.failure.Message += "; an earlier attempt of this Step may have sent the " + post.subject
			return sdkgo.NewMutationUncertain(post.requested, classification.failure, receipt)
		}
		// The refusal ends the Step, so a lost checkpoint cannot cause a resend.
		_ = fence.RecordCanResend()
		return sdkgo.NewMutationBranch(post.branches.providerRejected, post.requested, &classification.failure, receipt)
	case writeDefect:
		if hasPossibleEarlierSend {
			return sdkgo.NewMutationUncertain(post.requested, classification.failure, receipt)
		}
		return sdkgo.NewMutationBranch(post.branches.defect, post.requested, &classification.failure, receipt)
	}
	return client.resendOrSelectUncertain(post, canResendUnknownOutcome, classification.failure, classification.retryAfter, receipt)
}

// resendOrSelectUncertain retries an unknown outcome. A readable post always retries, because the
// next attempt reads it back first and sends again only below the cap.
func (client *Client) resendOrSelectUncertain(post messagePost, canResendUnknownOutcome bool, failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[PostMessageOutput] {
	if post.readBackPath != "" {
		// The claim stays the last marker, so the next attempt counts this send and reads back.
		return sdkgo.NewMutationRetry[PostMessageOutput](failure, max(retryAfter, ambiguousWriteSettleDelay))
	}
	if canResendUnknownOutcome {
		return sdkgo.NewMutationRetry[PostMessageOutput](failure, retryAfter)
	}
	failure.Message += "; the send cap is reached, so the " + post.subject + " is not sent again"
	return sdkgo.NewMutationUncertain(post.requested, failure, receipt)
}

// reconcileEarlierPost only reads the provider. When isResendAllowed, a definite absence or an
// inconclusive listing returns shouldResend instead of selecting uncertain.
func (client *Client) reconcileEarlierPost(session *operationSession, post messagePost, earliestSendAt time.Time, providerID string, isResendAllowed bool) (reconciled sdkgo.MutationAttempt[PostMessageOutput], shouldResend bool) {
	if post.readBackPath == "" {
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureTransport,
			"an earlier attempt of this Step may have sent the "+post.subject+", and the connection cannot read it back, so it is not sent again"),
			client.receipt(session, graphResponse{}, "")), false
	}
	if providerID != "" {
		if !messageIDPattern.MatchString(providerID) {
			return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureProtocol,
				"the earlier Microsoft Teams message has an invalid provider ID"), sdkgo.Receipt{}), false
		}
		result := client.exchange(session, graphRequest{method: http.MethodGet, path: post.readBackPath + "/" + providerID})
		classification := client.classifyRead(post.operationID, post.subject+" read-back", result)
		receipt := client.receipt(session, result.response, providerID)
		if classification.outcome == readRetry {
			return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, classification.retryAfter), false
		}
		if classification.outcome == readSucceeded {
			var resource chatMessageResource
			if json.Unmarshal(result.response.body, &resource) == nil && resource.ID == providerID {
				if message, err := decodeMessage(resource, 0); err == nil {
					return sdkgo.NewMutationBranch(post.branches.sent, postedOutput(post.requested, message, true), nil, receipt), false
				}
			}
		}
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureProtocol,
			"the earlier Microsoft Teams message could not be confirmed by its provider ID"), receipt), false
	}
	result := client.exchange(session, graphRequest{
		method: http.MethodGet, path: post.readBackPath, query: url.Values{"$top": {strconv.Itoa(readBackPageSize)}},
	})
	classification := client.classifyRead(post.operationID, post.subject+" read-back", result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, classification.retryAfter), false
	default:
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, classification.failure.Kind,
			"an earlier attempt of this Step sent the "+post.subject+" and it could not be read back, so it is not sent again: "+classification.failure.Message), receipt), isResendAllowed
	}
	var collection chatMessageCollection
	if err := json.Unmarshal(result.response.body, &collection); err != nil {
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureProtocol,
			"an earlier attempt of this Step sent the "+post.subject+", and the messages read back are invalid"), receipt), isResendAllowed
	}
	var matches []Message
	for _, resource := range collection.Value {
		if !post.isSentByThisStep(resource, earliestSendAt) {
			continue
		}
		if message, err := decodeMessage(resource, 0); err == nil {
			matches = append(matches, message)
		}
	}
	switch len(matches) {
	case 1:
		return sdkgo.NewMutationBranch(post.branches.sent, postedOutput(post.requested, matches[0], true), nil, client.receipt(session, result.response, matches[0].ID)), false
	case 0:
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureTransport,
			"an earlier attempt of this Step sent the "+post.subject+" without a confirmed outcome, and the newest messages show no match, so it is not sent again"), receipt), isResendAllowed
	default:
		// Several matches mean a duplicate may already exist, so another send is never allowed.
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureConflict,
			"an earlier attempt of this Step sent the "+post.subject+" without a confirmed outcome, and several identical messages were found, so it is not sent again"), receipt), false
	}
}

// isSentByThisStep requires a person's undeleted message, created after this Step's first send, in the same
// thread, with the requested subject and text.
func (post messagePost) isSentByThisStep(resource chatMessageResource, earliestSendAt time.Time) bool {
	if !messageIDPattern.MatchString(resource.ID) || resource.MessageType != "message" || resource.DeletedDateTime != nil {
		return false
	}
	if resource.From == nil || resource.From.User == nil || resource.Body == nil {
		return false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, resource.CreatedDateTime)
	if err != nil || createdAt.Before(earliestSendAt.Add(-writeAttributionSkew)) {
		return false
	}
	replyToID := ""
	if resource.ReplyToID != nil {
		replyToID = *resource.ReplyToID
	}
	if replyToID != post.readBackReplyToID {
		return false
	}
	subject := ""
	if resource.Subject != nil {
		subject = strings.TrimSpace(*resource.Subject)
	}
	if subject != strings.TrimSpace(post.content.body.Subject) {
		return false
	}
	return comparisonTextOf(extractMessageText(resource.Body.ContentType, resource.Body.Content)) == post.content.comparisonText
}
