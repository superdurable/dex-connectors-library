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

// send permits a repeat only after the immediately previous attempt proved the write was not applied.
func (client *Client) send(call sdkgo.Call, post messagePost) sdkgo.MutationAttempt[PostMessageOutput] {
	fence, err := singledispatch.NewFence(call, post.executeMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(post.branches.defect, post.requested,
			failurePointer(post.operationID, sdkgo.FailureLocalDefect, "Microsoft Teams dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureTransport,
			"the Microsoft Teams dispatch checkpoint is unavailable, so the message is not sent again"), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, post.operationID)
	if failure != nil {
		if decision.Kind == singledispatch.SendAllowed {
			checkpointErr := fence.RecordNotSent()
			credentialFailure := failure.failure
			if checkpointErr != nil {
				credentialFailure.Message += "; the not-sent checkpoint also failed"
			}
			if failure.isReauthorizationRequired {
				return sdkgo.NewMutationBranch(post.branches.defect, post.requested, &credentialFailure, sdkgo.Receipt{})
			}
			return sdkgo.NewMutationRetry[PostMessageOutput](credentialFailure, 0)
		}
		if failure.isReauthorizationRequired {
			return sdkgo.NewMutationUncertain(post.requested, failure.failure, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PostMessageOutput](failure.failure, 0)
	}
	defer cancel()
	if decision.Kind == singledispatch.ReadBackRequired {
		return client.reconcileEarlierPost(session, post, call.Context.FirstAttemptAt(), decision.Marker.ProviderID)
	}
	attempt := &messageSendAttempt{client: client, session: session, post: post}
	if err := fence.Dispatch(attempt.send); err != nil {
		if !errors.Is(err, singledispatch.ErrCheckpoint) && !errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureTransport,
				"Microsoft Teams message dispatch outcome is unknown"), sdkgo.Receipt{})
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
			if messageIDPattern.MatchString(resource.ID) {
				// Acceptance is known; a lost optional read-back checkpoint cannot permit another send.
				_ = fence.RecordProviderID(resource.ID)
			}
			if message, err := decodeMessage(resource, 0); err == nil {
				return sdkgo.NewMutationBranch(post.branches.sent, postedOutput(post.requested, message, false), nil, client.receipt(session, result.response, message.ID))
			}
		}
		classification.failure = newFailure(post.operationID, sdkgo.FailureProtocol, "Microsoft Graph accepted the "+post.subject+" but returned an unusable message")
	case writeNotApplied:
		if err := fence.RecordCanResend(); err != nil {
			classification.failure.Message += "; the safe-resend checkpoint failed"
		}
		return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, classification.retryAfter)
	case writeRejected:
		if result.response.statusCode == http.StatusUnauthorized && refreshFailure != nil && !sdkgo.IsReauthorizationRequired(refreshFailure) {
			classification.failure.Message = strings.TrimSuffix(classification.failure.Message, "; reconnect the connection") + "; credential refresh is temporarily unavailable"
		}
		return sdkgo.NewMutationBranch(post.branches.providerRejected, post.requested, &classification.failure, receipt)
	case writeDefect:
		return sdkgo.NewMutationBranch(post.branches.defect, post.requested, &classification.failure, receipt)
	}
	if post.readBackPath == "" {
		return sdkgo.NewMutationUncertain(post.requested, classification.failure, receipt)
	}
	// The checkpoint stays, so the next attempt reads the channel back instead of sending again.
	return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, max(classification.retryAfter, ambiguousWriteSettleDelay))
}

// reconcileEarlierPost only reads the provider; an inconclusive read never permits another POST.
func (client *Client) reconcileEarlierPost(session *operationSession, post messagePost, earliestSendAt time.Time, providerID string) sdkgo.MutationAttempt[PostMessageOutput] {
	if post.readBackPath == "" {
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureTransport,
			"an earlier attempt of this Step may have sent the "+post.subject+", and the connection cannot read it back, so it is not sent again"),
			client.receipt(session, graphResponse{}, ""))
	}
	if providerID != "" {
		if !messageIDPattern.MatchString(providerID) {
			return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureProtocol,
				"the earlier Microsoft Teams message has an invalid provider ID"), sdkgo.Receipt{})
		}
		result := client.exchange(session, graphRequest{method: http.MethodGet, path: post.readBackPath + "/" + providerID})
		classification := client.classifyRead(post.operationID, post.subject+" read-back", result)
		receipt := client.receipt(session, result.response, providerID)
		if classification.outcome == readRetry {
			return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, classification.retryAfter)
		}
		if classification.outcome == readSucceeded {
			var resource chatMessageResource
			if json.Unmarshal(result.response.body, &resource) == nil && resource.ID == providerID {
				if message, err := decodeMessage(resource, 0); err == nil {
					return sdkgo.NewMutationBranch(post.branches.sent, postedOutput(post.requested, message, true), nil, receipt)
				}
			}
		}
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureProtocol,
			"the earlier Microsoft Teams message could not be confirmed by its provider ID"), receipt)
	}
	result := client.exchange(session, graphRequest{
		method: http.MethodGet, path: post.readBackPath, query: url.Values{"$top": {strconv.Itoa(readBackPageSize)}},
	})
	classification := client.classifyRead(post.operationID, post.subject+" read-back", result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, classification.retryAfter)
	default:
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, classification.failure.Kind,
			"an earlier attempt of this Step sent the "+post.subject+" and it could not be read back, so it is not sent again: "+classification.failure.Message), receipt)
	}
	var collection chatMessageCollection
	if err := json.Unmarshal(result.response.body, &collection); err != nil {
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureProtocol,
			"an earlier attempt of this Step sent the "+post.subject+", and the messages read back are invalid"), receipt)
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
		return sdkgo.NewMutationBranch(post.branches.sent, postedOutput(post.requested, matches[0], true), nil, client.receipt(session, result.response, matches[0].ID))
	case 0:
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureTransport,
			"an earlier attempt of this Step sent the "+post.subject+" without a confirmed outcome, and the newest messages show no match, so it is not sent again"), receipt)
	default:
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureConflict,
			"an earlier attempt of this Step sent the "+post.subject+" without a confirmed outcome, and several identical messages were found, so it is not sent again"), receipt)
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
