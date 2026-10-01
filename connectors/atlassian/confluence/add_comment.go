// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

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
	addCommentOperationID    = "addComment"
	addCommentFailureSubject = "comment"
	// commentReconciliationLimit is how many of the page's newest footer comments a reconciling attempt reads.
	commentReconciliationLimit = 25
)

// AddCommentInput describes one footer comment on one page.
type AddCommentInput struct {
	// PageID is the numeric page ID, such as 123456.
	PageID string `json:"pageId"`
	// Body is the comment of at most 262144 characters in BodyFormat.
	Body string `json:"body"`
	// BodyFormat is the format of Body; blank reads Markdown.
	BodyFormat TextFormat `json:"bodyFormat,omitempty"`
}

// AddCommentOutput identifies the comment. On notFound, providerRejected, and uncertain, CommentID is
// empty and PageID echoes the request.
type AddCommentOutput struct {
	// PageID echoes the requested page.
	PageID string `json:"pageId"`
	// CommentID is the new footer comment's numeric ID.
	CommentID string `json:"commentId,omitempty"`
	// CreatedAt is when Confluence stored the comment.
	CreatedAt time.Time `json:"createdAt"`
	// IsConfirmedByReadBack reports that an earlier attempt sent the comment and the connector found it
	// among the page's newest footer comments instead of sending it again.
	IsConfirmedByReadBack bool `json:"isConfirmedByReadBack,omitempty"`
}

// AddCommentOperation implements the addComment Mutation.
type AddCommentOperation struct{ client *Client }

type createFooterCommentRequestBody struct {
	PageID string        `json:"pageId"`
	Body   pageBodyWrite `json:"body"`
}

type footerCommentResource struct {
	ID      string            `json:"id"`
	PageID  string            `json:"pageId"`
	Version *versionResource  `json:"version"`
	Body    *pageBodyResource `json:"body"`
}

type footerCommentListResource struct {
	Results []footerCommentResource `json:"results"`
}

// commentCreation is one validated comment with the text reconciliation compares against.
type commentCreation struct {
	body           createFooterCommentRequestBody
	requested      AddCommentOutput
	comparisonText string
}

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Confluence accepts no idempotency key; a Dex heartbeat checkpoint keeps the comment to one send.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends the comment at most once per Step execution. It records a dispatch checkpoint first; an
// attempt that finds the checkpoint never sends, and reports the earlier comment when the page's newest
// footer comments hold one with the same text, or selects uncertain. Only a 429 or a connection that
// never opened clears the checkpoint for a resend.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	client := operation.client
	creation, err := buildCommentCreation(input)
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, creation.requested, failurePointer(addCommentOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, addCommentOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[AddCommentOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, creation.requested, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	earliestSendAt := earliestDispatchTime(call, client.now())
	if dispatchedAt, hasCheckpoint := readDispatchCheckpoint(call); hasCheckpoint {
		return operation.reconcileEarlierDispatch(session, creation, earlierTime(earliestSendAt, dispatchedAt))
	}
	if err := recordDispatchCheckpoint(call, earliestSendAt); err != nil {
		return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "the comment checkpoint could not be recorded, so nothing was sent to Confluence"), 0)
	}
	result := client.exchange(session, confluenceRequest{method: http.MethodPost, api: contentAPI, path: "/footer-comments", payload: creation.body})
	classification := client.classifyWrite(addCommentOperationID, addCommentFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeAccepted:
		var comment footerCommentResource
		if json.Unmarshal(result.response.body, &comment) == nil && contentIDPattern.MatchString(comment.ID) {
			return sdkgo.NewMutationBranch(AddCommentBranchAdded, creation.addedOutput(comment, false), nil, client.receipt(session, result.response, comment.ID))
		}
		classification.failure = newFailure(addCommentOperationID, sdkgo.FailureProtocol, "Confluence accepted the comment but returned an unusable comment")
	case writeNotApplied:
		clearDispatchCheckpoint(call)
		return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, classification.retryAfter)
	case writeNotFound:
		return sdkgo.NewMutationBranch(AddCommentBranchNotFound, creation.requested, &classification.failure, receipt)
	case writeRejected:
		return sdkgo.NewMutationBranch(AddCommentBranchProviderRejected, creation.requested, &classification.failure, receipt)
	case writeDefect:
		clearDispatchCheckpoint(call)
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, creation.requested, &classification.failure, receipt)
	}
	// The checkpoint stays, so the next attempt reads the page's comments instead of sending again.
	return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, max(classification.retryAfter, ambiguousWriteSettleDelay))
}

// reconcileEarlierDispatch never sends: it reports an earlier attempt's comment found on the page, or uncertain.
func (operation AddCommentOperation) reconcileEarlierDispatch(session *operationSession, creation commentCreation, earliestSendAt time.Time) sdkgo.MutationAttempt[AddCommentOutput] {
	client := operation.client
	result := client.exchange(session, confluenceRequest{
		method: http.MethodGet, api: contentAPI, path: pagePath(creation.body.PageID) + "/footer-comments",
		query: url.Values{"body-format": {string(BodyRepresentationStorage)}, "sort": {"-created-date"}, "limit": {strconv.Itoa(commentReconciliationLimit)}},
	})
	classification := client.classifyRead(addCommentOperationID, "page comments", result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewMutationBranch(AddCommentBranchNotFound, creation.requested, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationUncertain(creation.requested, newFailure(addCommentOperationID, classification.failure.Kind,
			"an earlier attempt of this Step sent the comment, and the page's comments could not be read back: "+classification.failure.Message), receipt)
	}
	var list footerCommentListResource
	if err := json.Unmarshal(result.response.body, &list); err != nil {
		return sdkgo.NewMutationUncertain(creation.requested, newFailure(addCommentOperationID, sdkgo.FailureProtocol,
			"an earlier attempt of this Step sent the comment, and the page's comments read back are invalid"), receipt)
	}
	for _, comment := range list.Results {
		if creation.isSentByThisStep(comment, earliestSendAt) {
			return sdkgo.NewMutationBranch(AddCommentBranchAdded, creation.addedOutput(comment, true), nil, client.receipt(session, result.response, comment.ID))
		}
	}
	return sdkgo.NewMutationUncertain(creation.requested, newFailure(addCommentOperationID, sdkgo.FailureTransport,
		"an earlier attempt of this Step sent the comment without a confirmed outcome, and the page shows no matching comment, so it is not sent again"), receipt)
}

func buildCommentCreation(input AddCommentInput) (commentCreation, error) {
	creation := commentCreation{requested: AddCommentOutput{PageID: strings.TrimSpace(input.PageID)}}
	pageID, err := validateContentID(input.PageID, "pageId")
	if err != nil {
		return creation, err
	}
	storage, comparisonText, err := convertWrittenBody(input.Body, input.BodyFormat, "body")
	if err != nil {
		return creation, err
	}
	creation.comparisonText = comparisonText
	creation.body = createFooterCommentRequestBody{PageID: pageID, Body: pageBodyWrite{Representation: string(BodyRepresentationStorage), Value: storage}}
	return creation, nil
}

// isSentByThisStep requires a comment created after this Step's first send with the requested text.
func (creation commentCreation) isSentByThisStep(comment footerCommentResource, earliestSendAt time.Time) bool {
	if !contentIDPattern.MatchString(comment.ID) || comment.Version == nil || comment.Body == nil || comment.Body.Storage == nil {
		return false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, comment.Version.CreatedAt)
	if err != nil || createdAt.Before(earliestSendAt.Add(-writeAttributionSkew)) {
		return false
	}
	return storageComparisonText(comment.Body.Storage.Value) == creation.comparisonText
}

func (creation commentCreation) addedOutput(comment footerCommentResource, isConfirmedByReadBack bool) AddCommentOutput {
	output := creation.requested
	output.PageID, output.CommentID, output.IsConfirmedByReadBack = creation.body.PageID, comment.ID, isConfirmedByReadBack
	if comment.Version != nil {
		// The comment exists either way; an unparseable timestamp only leaves CreatedAt zero.
		output.CreatedAt, _ = time.Parse(time.RFC3339Nano, comment.Version.CreatedAt)
	}
	return output
}
