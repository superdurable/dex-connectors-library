// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetMessageInput identifies a Front message.
type GetMessageInput struct {
	// MessageID is the Front msg_ identifier to read.
	MessageID string `json:"messageId"`
}

// ListConversationMessagesInput selects a bounded page of a conversation's messages.
type ListConversationMessagesInput struct {
	// ConversationID is the Front cnv_ identifier containing the messages.
	ConversationID string `json:"conversationId"`
	// PageSize is 1 to 100 messages; zero requests 25.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is an opaque continuation token from a prior page; blank starts at the newest page.
	PageToken string `json:"pageToken,omitempty"`
}

// MessagesPage contains one message page and its opaque continuation token.
type MessagesPage struct {
	// Messages are the bounded page Front returned.
	Messages []Message `json:"messages"`
	// NextPageToken requests the next older page; blank means there is no next page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListConversationCommentsInput identifies the conversation whose internal comments to read.
type ListConversationCommentsInput struct {
	// ConversationID is the Front cnv_ identifier containing the comments.
	ConversationID string `json:"conversationId"`
}

// CommentsPage contains a conversation's internal comments.
type CommentsPage struct {
	// Comments are the internal comments Front returned.
	Comments []Comment `json:"comments"`
}

// GetMessageOperation reads one message.
type GetMessageOperation struct{ client *Client }

// ListConversationMessagesOperation reads a bounded message page.
type ListConversationMessagesOperation struct{ client *Client }

// ListConversationCommentsOperation reads the internal comment list.
type ListConversationCommentsOperation struct{ client *Client }

// GetMessage binds the query to this client.
func (client *Client) GetMessage() GetMessageOperation { return GetMessageOperation{client} }

// ListConversationMessages binds the query to this client.
func (client *Client) ListConversationMessages() ListConversationMessagesOperation {
	return ListConversationMessagesOperation{client}
}

// ListConversationComments binds the query to this client.
func (client *Client) ListConversationComments() ListConversationCommentsOperation {
	return ListConversationCommentsOperation{client}
}

// Definition returns the generated operation contract.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Definition returns the generated operation contract.
func (ListConversationMessagesOperation) Definition() sdkgo.QueryDefinition {
	return ListConversationMessagesDefinition
}

// Definition returns the generated operation contract.
func (ListConversationCommentsOperation) Definition() sdkgo.QueryDefinition {
	return ListConversationCommentsDefinition
}

// Invoke reads a Front message by ID.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[Message] {
	operationID := GetMessageDefinition.Operation.OperationID
	if !messageIDPattern.MatchString(input.MessageID) {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, frontFailurePointer(sdkgo.FailureValidation, operationID, "messageId must be a Front msg_ ID"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: "/messages/" + input.MessageID})
	receipt := operation.client.receipt(call, input.MessageID)
	switch {
	case result.isRetryable():
		return sdkgo.NewQueryRetry[Message](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(GetMessageBranchNotFound, Message{}, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, &result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, &result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return sdkgo.NewQueryBranch(GetMessageBranchProviderRejected, Message{}, &result.failure, receipt)
	}
	var wire frontMessageWire
	if json.Unmarshal(result.response.body, &wire) != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid message"), receipt)
	}
	message, err := decodeMessage(wire)
	if err != nil || message.ID != input.MessageID {
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned a message with an invalid identity"), receipt)
	}
	return sdkgo.NewQueryBranch(GetMessageBranchFound, message, nil, receipt)
}

// Invoke lists one bounded page of conversation messages.
func (operation ListConversationMessagesOperation) Invoke(call sdkgo.Call, input ListConversationMessagesInput) sdkgo.QueryAttempt[MessagesPage] {
	operationID := ListConversationMessagesDefinition.Operation.OperationID
	output := MessagesPage{Messages: []Message{}}
	if !conversationIDPattern.MatchString(input.ConversationID) || input.PageSize < 0 || input.PageSize > 100 || (input.PageToken != "" && !pageTokenPattern.MatchString(input.PageToken)) {
		return sdkgo.NewQueryBranch(ListConversationMessagesBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, "conversationId, pageSize, or pageToken is invalid"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListConversationMessagesBranchDefect, output, failure, sdkgo.Receipt{})
	}
	path := "/conversations/" + input.ConversationID + "/messages"
	parameters := url.Values{"limit": {strconv.Itoa(pageSizeOrDefault(input.PageSize))}}
	if input.PageToken != "" {
		parameters.Set("page_token", input.PageToken)
	}
	result := operation.client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: path, query: parameters})
	receipt := operation.client.receipt(call, input.ConversationID)
	switch {
	case result.isRetryable():
		return sdkgo.NewQueryRetry[MessagesPage](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(ListConversationMessagesBranchNotFound, output, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(ListConversationMessagesBranchDefect, output, &result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(ListConversationMessagesBranchInvalidResponse, output, &result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return sdkgo.NewQueryBranch(ListConversationMessagesBranchProviderRejected, output, &result.failure, receipt)
	}
	var page frontPageWire[frontMessageWire]
	if json.Unmarshal(result.response.body, &page) != nil || page.Results == nil {
		return sdkgo.NewQueryBranch(ListConversationMessagesBranchInvalidResponse, output, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid message page"), receipt)
	}
	for _, wire := range page.Results {
		message, err := decodeMessage(wire)
		if err != nil {
			return sdkgo.NewQueryBranch(ListConversationMessagesBranchInvalidResponse, MessagesPage{Messages: []Message{}}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid message"), receipt)
		}
		output.Messages = append(output.Messages, message)
	}
	var err error
	output.NextPageToken, err = operation.client.nextPageToken(page.Pagination.Next, path)
	if err != nil {
		return sdkgo.NewQueryBranch(ListConversationMessagesBranchInvalidResponse, MessagesPage{Messages: []Message{}}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid next-page link"), receipt)
	}
	return sdkgo.NewQueryBranch(ListConversationMessagesBranchListed, output, nil, receipt)
}

// Invoke lists a conversation's internal comments.
func (operation ListConversationCommentsOperation) Invoke(call sdkgo.Call, input ListConversationCommentsInput) sdkgo.QueryAttempt[CommentsPage] {
	operationID := ListConversationCommentsDefinition.Operation.OperationID
	output := CommentsPage{Comments: []Comment{}}
	if !conversationIDPattern.MatchString(input.ConversationID) {
		return sdkgo.NewQueryBranch(ListConversationCommentsBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, "conversationId must be a Front cnv_ ID"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListConversationCommentsBranchDefect, output, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: "/conversations/" + input.ConversationID + "/comments"})
	receipt := operation.client.receipt(call, input.ConversationID)
	switch {
	case result.isRetryable():
		return sdkgo.NewQueryRetry[CommentsPage](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(ListConversationCommentsBranchNotFound, output, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(ListConversationCommentsBranchDefect, output, &result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(ListConversationCommentsBranchInvalidResponse, output, &result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return sdkgo.NewQueryBranch(ListConversationCommentsBranchProviderRejected, output, &result.failure, receipt)
	}
	var page frontPageWire[frontCommentWire]
	if json.Unmarshal(result.response.body, &page) != nil || page.Results == nil {
		return sdkgo.NewQueryBranch(ListConversationCommentsBranchInvalidResponse, output, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid comment list"), receipt)
	}
	for _, wire := range page.Results {
		comment, err := decodeComment(wire)
		if err != nil {
			return sdkgo.NewQueryBranch(ListConversationCommentsBranchInvalidResponse, CommentsPage{Comments: []Comment{}}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid comment"), receipt)
		}
		output.Comments = append(output.Comments, comment)
	}
	return sdkgo.NewQueryBranch(ListConversationCommentsBranchListed, output, nil, receipt)
}
