// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	addCommentOperationID = "addComment"

	commentFields      = `id url createdAt`
	addCommentDocument = `mutation LinearAddComment($input: CommentCreateInput!) {
  commentCreate(input: $input) { success comment { ` + commentFields + ` } }
}`
	readCommentDocument = `query LinearReadComment($filter: CommentFilter!) {
  comments(filter: $filter, first: 1) { nodes { ` + commentFields + ` } }
}`
)

// AddCommentInput describes one comment.
type AddCommentInput struct {
	// IssueID is the issue's UUID or its identifier, such as ENG-123.
	IssueID string `json:"issueId"`
	// Body is the comment's Markdown, 1 to MaxMarkdownCharacters characters.
	Body string `json:"body"`
}

// AddCommentOutput is the added comment. On every other branch only IssueID is set.
type AddCommentOutput struct {
	// IssueID echoes the requested issue UUID or identifier.
	IssueID string `json:"issueId"`
	// CommentID is the comment's UUID, the client-supplied ID every attempt of the Step sent.
	CommentID string `json:"commentId,omitempty"`
	// URL is the comment's link in Linear.
	URL string `json:"url,omitempty"`
	// CreatedAt is when Linear created the comment.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// IsReplayed reports that an earlier attempt of this Step had already added the comment, which this
	// attempt read back by its UUID.
	IsReplayed bool `json:"replayed,omitempty"`
}

// AddCommentOperation implements the addComment Mutation.
type AddCommentOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey is the stable Call ID, from which every attempt derives the same comment UUID.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds the comment under the Step's client-supplied UUID and, after any answer other than a usable
// comment, a rate limit, a credential or permission rejection, or a rejected GraphQL document, reads it
// back by that UUID before deciding; a failed read-back is retried.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	requested := AddCommentOutput{IssueID: input.IssueID}
	request, locator, commentID, err := buildAddCommentRequest(call.IdempotencyKey, input)
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, linearFailurePointer(sdkgo.FailureValidation, addCommentOperationID, err.Error()), sdkgo.Receipt{})
	}
	requested.IssueID = locator.mutationID()
	session, cancel, failed := operation.client.openSession(call, addCommentOperationID)
	defer cancel()
	if failed != nil {
		return writeAttemptForExchange(*failed, requested, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName},
			AddCommentBranchProviderRejected, AddCommentBranchDefect)
	}
	result := session.exchange(request)
	receipt := session.receipt(result.response, commentID)
	if payload, isPresent := decodeMutationPayload(result.response.data, "commentCreate"); isPresent && payload.Success != nil && *payload.Success {
		if comment, err := decodeComment(payload.Comment, commentID); err == nil {
			return addedCommentAttempt(requested, comment, false, receipt)
		}
	}
	result = withUnusablePayloadClassified(result, "commentCreate", addCommentOperationID)
	if !isReadBackWorthy(result) {
		return writeAttemptForExchange(result, requested, receipt, AddCommentBranchProviderRejected, AddCommentBranchDefect)
	}
	commentRead := session.exchange(graphQLRequest{operationName: "LinearReadComment", document: readCommentDocument,
		variables: map[string]any{"filter": map[string]any{"id": map[string]any{"eq": commentID}}}})
	comment, isFound, readErr := decodeCommentNodes(commentRead, commentID)
	switch {
	case commentRead.outcome != exchangeSucceeded || readErr != nil:
		return sdkgo.NewMutationRetry[AddCommentOutput](readFailureOf(commentRead, addCommentOperationID), commentRead.retryAfter)
	case isFound:
		return addedCommentAttempt(requested, comment, true, session.receipt(commentRead.response, commentID))
	case result.outcome != exchangeRejected:
		return writeAttemptForExchange(result, requested, receipt, AddCommentBranchProviderRejected, AddCommentBranchDefect)
	}
	issueRead := readIssueSummary(session, locator)
	switch {
	case issueRead.result.outcome == exchangeRetry:
		return sdkgo.NewMutationRetry[AddCommentOutput](issueRead.result.failure, issueRead.result.retryAfter)
	case issueRead.result.outcome == exchangeSucceeded && !issueRead.isFound:
		return sdkgo.NewMutationBranch(AddCommentBranchNotFound, requested, linearFailurePointer(sdkgo.FailureNotFound, addCommentOperationID,
			"Linear has no issue with this ID that the connection can see"), receipt)
	}
	return writeAttemptForExchange(result, requested, receipt, AddCommentBranchProviderRejected, AddCommentBranchDefect)
}

func addedCommentAttempt(requested AddCommentOutput, comment addedComment, isReplayed bool, receipt sdkgo.Receipt) sdkgo.MutationAttempt[AddCommentOutput] {
	output := requested
	output.CommentID, output.URL, output.CreatedAt, output.IsReplayed = comment.id, comment.url, &comment.createdAt, isReplayed
	receipt.ProviderObjectID = comment.id
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt)
}

func buildAddCommentRequest(key sdkgo.IdempotencyKey, input AddCommentInput) (graphQLRequest, issueLocator, string, error) {
	locator, err := parseIssueLocator(input.IssueID, "issueId")
	if err != nil {
		return graphQLRequest{}, issueLocator{}, "", err
	}
	body, err := validateMarkdown(input.Body, "body", true)
	if err != nil {
		return graphQLRequest{}, locator, "", err
	}
	commentID, err := clientEntityID(key, "comment")
	if err != nil {
		return graphQLRequest{}, locator, "", err
	}
	return graphQLRequest{operationName: "LinearAddComment", document: addCommentDocument, variables: map[string]any{
		"input": map[string]any{"id": commentID, "issueId": locator.mutationID(), "body": body},
	}}, locator, commentID, nil
}

// addedComment is a decoded comment whose ID is the Step's client-supplied UUID.
type addedComment struct {
	id        string
	url       string
	createdAt time.Time
}

type commentWire struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
}

// decodeComment reads one comment and requires the client-supplied UUID.
func decodeComment(raw json.RawMessage, commentID string) (addedComment, error) {
	var wire commentWire
	if isJSONNull(raw) || json.Unmarshal(raw, &wire) != nil || !strings.EqualFold(wire.ID, commentID) {
		return addedComment{}, errors.New("Linear returned a malformed comment or another comment than requested")
	}
	createdAt, err := parseLinearTime(wire.CreatedAt)
	if err != nil {
		return addedComment{}, errors.New("Linear returned a comment without a creation time")
	}
	return addedComment{id: commentID, url: wire.URL, createdAt: createdAt}, nil
}

// decodeCommentNodes reads a comments read-back; it reports an error only for a malformed 2xx answer.
func decodeCommentNodes(result linearExchange, commentID string) (addedComment, bool, error) {
	if result.outcome != exchangeSucceeded {
		return addedComment{}, false, nil
	}
	var document struct {
		Comments *struct {
			Nodes []json.RawMessage `json:"nodes"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(result.response.data, &document); err != nil || document.Comments == nil || len(document.Comments.Nodes) > 1 {
		return addedComment{}, false, errors.New("Linear returned a malformed comment list")
	}
	if len(document.Comments.Nodes) == 0 {
		return addedComment{}, false, nil
	}
	comment, err := decodeComment(document.Comments.Nodes[0], commentID)
	return comment, err == nil, err
}

// readFailureOf describes a read-back that must be retried, including a malformed answer.
func readFailureOf(result linearExchange, operation string) sdkgo.Failure {
	if result.outcome != exchangeSucceeded {
		return result.failure
	}
	return linearFailure(sdkgo.FailureProtocol, operation, "Linear returned an unusable comment read-back")
}
