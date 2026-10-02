// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
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
	operationID string
	subject     string
	path        string
	content     messageContent
	requested   PostMessageOutput
	branches    postBranches
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

// send posts the message at most once per Step execution. It records a dispatch checkpoint first; an
// attempt that finds the checkpoint never sends, and reports the earlier message when a read-back finds
// exactly one match, or selects uncertain. Only a 429 or a connection that never opened clears the
// checkpoint for a resend.
func (client *Client) send(call sdkgo.Call, post messagePost) sdkgo.MutationAttempt[PostMessageOutput] {
	session, cancel, failure := client.startSession(call, post.operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(post.branches.defect, post.requested, failure, sdkgo.Receipt{})
	}
	defer cancel()
	earliestSendAt := earliestDispatchTime(call, client.now())
	if dispatchedAt, hasCheckpoint := readDispatchCheckpoint(call); hasCheckpoint {
		return client.reconcileEarlierPost(session, post, earlierTime(earliestSendAt, dispatchedAt))
	}
	if err := recordDispatchCheckpoint(call, earliestSendAt); err != nil {
		return sdkgo.NewMutationRetry[PostMessageOutput](newFailure(post.operationID, sdkgo.FailureAvailability,
			"the "+post.subject+" checkpoint could not be recorded, so nothing was sent to Microsoft Teams"), 0)
	}
	result := client.exchange(session, graphRequest{method: http.MethodPost, path: post.path, payload: post.content.body})
	classification := client.classifyWrite(post.operationID, post.subject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeAccepted:
		var resource chatMessageResource
		if json.Unmarshal(result.response.body, &resource) == nil {
			if message, err := decodeMessage(resource, 0); err == nil {
				return sdkgo.NewMutationBranch(post.branches.sent, postedOutput(post.requested, message, false), nil, client.receipt(session, result.response, message.ID))
			}
		}
		classification.failure = newFailure(post.operationID, sdkgo.FailureProtocol, "Microsoft Graph accepted the "+post.subject+" but returned an unusable message")
	case writeNotApplied:
		clearDispatchCheckpoint(call)
		return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, classification.retryAfter)
	case writeRejected:
		return sdkgo.NewMutationBranch(post.branches.providerRejected, post.requested, &classification.failure, receipt)
	case writeDefect:
		clearDispatchCheckpoint(call)
		return sdkgo.NewMutationBranch(post.branches.defect, post.requested, &classification.failure, receipt)
	}
	if post.readBackPath == "" {
		return sdkgo.NewMutationUncertain(post.requested, classification.failure, receipt)
	}
	// The checkpoint stays, so the next attempt reads the channel back instead of sending again.
	return sdkgo.NewMutationRetry[PostMessageOutput](classification.failure, max(classification.retryAfter, ambiguousWriteSettleDelay))
}

// reconcileEarlierPost never sends: it reports an earlier attempt's message found by a read-back, or uncertain.
func (client *Client) reconcileEarlierPost(session *operationSession, post messagePost, earliestSendAt time.Time) sdkgo.MutationAttempt[PostMessageOutput] {
	if post.readBackPath == "" {
		return sdkgo.NewMutationUncertain(post.requested, newFailure(post.operationID, sdkgo.FailureTransport,
			"an earlier attempt of this Step may have sent the "+post.subject+", and the connection cannot read it back, so it is not sent again"),
			client.receipt(session, graphResponse{}, ""))
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
