// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

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
// Confluence accepts no idempotency key; a Dex heartbeat checkpoint counts each send of the comment.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends the comment behind the shared single-dispatch fence. An attempt after a possible earlier
// send first reads the page's newest footer comments and reports one with the same text; otherwise it
// resends while possible sends stay within the send cap, and selects uncertain at the cap.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	client := operation.client
	creation, err := buildCommentCreation(input)
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, creation.requested, failurePointer(addCommentOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, AddCommentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, creation.requested, failurePointer(addCommentOperationID, sdkgo.FailureLocalDefect, "Confluence dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "the comment checkpoint is unreadable, so nothing was sent in this attempt"), 0)
	}
	// An earlier send of this call may have taken effect, so a refusal or defect proves nothing.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	session, cancel, sessionErr := client.startSession(call, addCommentOperationID)
	if sessionErr != nil {
		// notSent after a possible earlier send would hide it, so only a first-send decision records it.
		if decision.Kind == singledispatch.SendAllowed {
			if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
				return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Confluence setup failed and the not-sent checkpoint could not be recorded"), sessionErr.retryAfter)
			}
		}
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[AddCommentOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return commentRefusalOrUncertain(hasPossibleEarlierSend, AddCommentBranchDefect, creation.requested, sessionErr.failure, sdkgo.Receipt{})
	}
	defer cancel()
	earliestSendAt := earliestDispatchTime(call, client.now())
	switch {
	case decision.Kind == singledispatch.ReadBackRequired, decision.Marker.ProviderID != "":
		attempt, isMissing, readReceipt := operation.readBackEarlierComment(session, creation, earliestSendAt)
		if isMissing {
			return sdkgo.NewMutationUncertain(creation.requested, newFailure(addCommentOperationID, sdkgo.FailureTransport,
				"earlier attempts of this Step sent the comment up to the send cap, and the page shows no matching comment, so it is not sent again"), readReceipt)
		}
		return attempt
	case decision.Kind == singledispatch.ReadBackThenResendAllowed:
		if attempt, isMissing, _ := operation.readBackEarlierComment(session, creation, earliestSendAt); !isMissing {
			return attempt
		}
	}
	var result exchangeResult
	err = fence.Dispatch(func(requestContext context.Context) error {
		fencedSession := *session
		fencedSession.context = requestContext
		result = client.exchange(&fencedSession, confluenceRequest{method: http.MethodPost, api: contentAPI, path: "/footer-comments", payload: creation.body})
		return nil
	})
	switch {
	case errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime):
		return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "the comment checkpoint or request budget is unavailable, so nothing was sent to Confluence"), 0)
	case err != nil:
		return commentRefusalOrUncertain(hasPossibleEarlierSend, AddCommentBranchDefect, creation.requested, newFailure(addCommentOperationID, sdkgo.FailureLocalDefect, "Confluence dispatch was not allowed"), sdkgo.Receipt{})
	}
	classification := client.classifyWrite(addCommentOperationID, addCommentFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeAccepted:
		var comment footerCommentResource
		if json.Unmarshal(result.response.body, &comment) == nil && contentIDPattern.MatchString(comment.ID) {
			// The accepted comment ID keeps a replayed attempt from resending; a lost checkpoint keeps the claim.
			_ = fence.RecordProviderID(comment.ID)
			return sdkgo.NewMutationBranch(AddCommentBranchAdded, creation.addedOutput(comment, false), nil, client.receipt(session, result.response, comment.ID))
		}
		classification.failure = newFailure(addCommentOperationID, sdkgo.FailureProtocol, "Confluence accepted the comment but returned an unusable comment")
	case writeNotApplied:
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Confluence retry checkpoint could not be recorded"), classification.retryAfter)
		}
		return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, classification.retryAfter)
	case writeNotFound, writeRejected:
		branch := AddCommentBranchProviderRejected
		if classification.outcome == writeNotFound {
			branch = AddCommentBranchNotFound
		}
		if !hasPossibleEarlierSend {
			if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
				return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Confluence rejection checkpoint could not be recorded"), 0)
			}
		}
		return commentRefusalOrUncertain(hasPossibleEarlierSend, branch, creation.requested, classification.failure, receipt)
	case writeDefect:
		// The request was never built, so nothing was sent; a lost checkpoint only keeps the claim.
		_ = fence.RecordCanResend()
		return commentRefusalOrUncertain(hasPossibleEarlierSend, AddCommentBranchDefect, creation.requested, classification.failure, receipt)
	}
	// The claim stays, so the next attempt reads the page's comments, then resends within the cap or selects uncertain.
	return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, max(classification.retryAfter, ambiguousWriteSettleDelay))
}

// commentRefusalOrUncertain selects a definite branch only when no earlier send may have added the comment.
func commentRefusalOrUncertain(hasPossibleEarlierSend bool, branch sdkgo.BranchID, requested AddCommentOutput, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[AddCommentOutput] {
	if hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(requested, failure, receipt)
	}
	return sdkgo.NewMutationBranch(branch, requested, &failure, receipt)
}

// readBackEarlierComment never sends: it reports an earlier attempt's comment found on the page, a Retry
// for a transient read, or a missing page. isMissing means read-back could not confirm the comment.
func (operation AddCommentOperation) readBackEarlierComment(session *operationSession, creation commentCreation, earliestSendAt time.Time) (sdkgo.MutationAttempt[AddCommentOutput], bool, sdkgo.Receipt) {
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
		return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, classification.retryAfter), false, receipt
	case readNotFound:
		return sdkgo.NewMutationBranch(AddCommentBranchNotFound, creation.requested, &classification.failure, receipt), false, receipt
	default:
		return sdkgo.MutationAttempt[AddCommentOutput]{}, true, receipt
	}
	var list footerCommentListResource
	if err := json.Unmarshal(result.response.body, &list); err != nil {
		return sdkgo.MutationAttempt[AddCommentOutput]{}, true, receipt
	}
	for _, comment := range list.Results {
		if creation.isSentByThisStep(comment, earliestSendAt) {
			return sdkgo.NewMutationBranch(AddCommentBranchAdded, creation.addedOutput(comment, true), nil, client.receipt(session, result.response, comment.ID)), false, receipt
		}
	}
	return sdkgo.MutationAttempt[AddCommentOutput]{}, true, receipt
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
