// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultMessageLimit is how many newest messages a zero GetConversationInput.MessageLimit reads.
	DefaultMessageLimit = 10
	// MaxMessageLimit bounds GetConversationInput.MessageLimit.
	MaxMessageLimit = 25
	// DefaultCommentLimit is how many newest comments a zero GetConversationInput.CommentLimit returns.
	DefaultCommentLimit = 10
	// MaxCommentLimit bounds GetConversationInput.CommentLimit.
	MaxCommentLimit = 25
)

// GetConversationInput identifies one conversation and how much of its history to return.
type GetConversationInput struct {
	// ConversationID is the Front conversation ID, such as cnv_55c8c149.
	ConversationID string `json:"conversationId"`
	// MessageLimit is 1 to MaxMessageLimit newest messages; zero reads DefaultMessageLimit.
	MessageLimit int `json:"messageLimit,omitempty"`
	// CommentLimit is 1 to MaxCommentLimit newest internal comments; zero returns DefaultCommentLimit.
	CommentLimit int `json:"commentLimit,omitempty"`
}

// ConversationDetails is one conversation with its newest messages and comments, each newest first.
type ConversationDetails struct {
	// Conversation is the conversation; it is empty on the merged branch.
	Conversation Conversation `json:"conversation"`
	// Messages are the newest messages, newest first.
	Messages []Message `json:"messages"`
	// HasOlderMessages reports that Front holds older messages than Messages.
	HasOlderMessages bool `json:"hasOlderMessages,omitempty"`
	// Comments are the newest internal comments, newest first.
	Comments []Comment `json:"comments"`
	// HasOlderComments reports that the conversation holds older comments than Comments.
	HasOlderComments bool `json:"hasOlderComments,omitempty"`
	// MergedIntoConversationID is the conversation that absorbed the requested one; set only on the merged branch.
	MergedIntoConversationID string `json:"mergedIntoConversationId,omitempty"`
}

// GetConversationOperation implements the getConversation Query. Build it with Client.GetConversation.
type GetConversationOperation struct{ client *Client }

type frontMessageWire struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	IsInbound  bool                 `json:"is_inbound"`
	DraftMode  *string              `json:"draft_mode"`
	Subject    string               `json:"subject"`
	Blurb      string               `json:"blurb"`
	Body       string               `json:"body"`
	Author     *frontTeammateWire   `json:"author"`
	Recipients []frontRecipientWire `json:"recipients"`
	CreatedAt  frontTimestamp       `json:"created_at"`
}

type frontCommentWire struct {
	ID       string             `json:"id"`
	Body     string             `json:"body"`
	Author   *frontTeammateWire `json:"author"`
	IsPinned bool               `json:"is_pinned"`
	PostedAt frontTimestamp     `json:"posted_at"`
}

// Definition returns the immutable getConversation operation definition.
func (GetConversationOperation) Definition() sdkgo.QueryDefinition {
	return GetConversationDefinition
}

// Invoke reads GET /conversations/{id}, the first page of its messages, and its comments. A merged
// conversation answers 301, which is never followed and selects merged.
func (operation GetConversationOperation) Invoke(call sdkgo.Call, input GetConversationInput) sdkgo.QueryAttempt[ConversationDetails] {
	operationID := GetConversationDefinition.Operation.OperationID
	client := operation.client
	output := ConversationDetails{Messages: []Message{}, Comments: []Comment{}}
	if err := validateGetConversationInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, output, failure, sdkgo.Receipt{})
	}
	conversationPath := "/conversations/" + input.ConversationID
	reads := []frontRequest{
		{method: http.MethodGet, path: conversationPath},
		{method: http.MethodGet, path: conversationPath + "/messages", query: url.Values{"limit": {strconv.Itoa(cmp.Or(input.MessageLimit, DefaultMessageLimit))}}},
		{method: http.MethodGet, path: conversationPath + "/comments"},
	}
	receipt := client.receipt(call, input.ConversationID)
	for index, request := range reads {
		result := client.exchange(call.Context, credentials, operationID, request)
		if attempt, isTerminal := classifyConversationRead(result, output, receipt); isTerminal {
			return attempt
		}
		var err error
		switch index {
		case 0:
			output.Conversation, err = decodeConversation(result.response.body)
			if err == nil && output.Conversation.ID != input.ConversationID {
				err = errors.New("Front returned another conversation")
			}
		case 1:
			err = client.decodeMessagePage(result.response.body, input.ConversationID, &output)
		default:
			err = decodeComments(result.response.body, cmp.Or(input.CommentLimit, DefaultCommentLimit), &output)
		}
		if err != nil {
			return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{Messages: []Message{}, Comments: []Comment{}},
				frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid conversation: "+err.Error()), receipt)
		}
	}
	return sdkgo.NewQueryBranch(GetConversationBranchFound, output, nil, receipt)
}

// classifyConversationRead maps every outcome but success to a Retry or a terminal branch.
func classifyConversationRead(
	result frontExchange, output ConversationDetails, receipt sdkgo.Receipt,
) (sdkgo.QueryAttempt[ConversationDetails], bool) {
	switch {
	case result.outcome == exchangeSucceeded:
		return sdkgo.QueryAttempt[ConversationDetails]{}, false
	case result.isRetryable():
		return sdkgo.NewQueryRetry[ConversationDetails](result.failure, result.retryAfter), true
	case result.outcome == exchangeMerged:
		output.MergedIntoConversationID = mergedConversationID(result.response.header.Get("Location"))
		return sdkgo.NewQueryBranch(GetConversationBranchMerged, output, &result.failure, receipt), true
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(GetConversationBranchNotFound, output, &result.failure, receipt), true
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, output, &result.failure, sdkgo.Receipt{}), true
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, output, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(GetConversationBranchProviderRejected, output, &result.failure, receipt), true
	}
}

func (client *Client) decodeMessagePage(body []byte, conversationID string, output *ConversationDetails) error {
	var page frontPageWire[frontMessageWire]
	if err := json.Unmarshal(body, &page); err != nil || page.Results == nil {
		return errors.New("the message page is not a message list")
	}
	for _, wire := range page.Results {
		if !messageIDPattern.MatchString(wire.ID) {
			return errors.New("a message lacks a valid ID")
		}
		author, err := convertTeammate(wire.Author)
		if err != nil {
			return err
		}
		message := Message{
			ID: wire.ID, Type: wire.Type, IsInbound: wire.IsInbound, IsDraft: wire.DraftMode != nil && *wire.DraftMode != "",
			Subject: wire.Subject, Blurb: wire.Blurb, Author: author, CreatedAt: wire.CreatedAt.instant,
		}
		message.Body, message.IsBodyTruncated = truncateUTF8(wire.Body, MaxBodyBytes)
		for _, recipient := range wire.Recipients {
			message.Recipients = append(message.Recipients, convertRecipient(recipient))
		}
		output.Messages = append(output.Messages, message)
	}
	token, err := client.nextPageToken(page.Pagination.Next, "/conversations/"+conversationID+"/messages")
	output.HasOlderMessages = token != ""
	return err
}

// decodeComments keeps the newest limit comments; Front lists every comment, newest first.
func decodeComments(body []byte, limit int, output *ConversationDetails) error {
	var page frontPageWire[frontCommentWire]
	if err := json.Unmarshal(body, &page); err != nil || page.Results == nil {
		return errors.New("the comments answer has no _results list")
	}
	for _, wire := range page.Results {
		if len(output.Comments) == limit {
			output.HasOlderComments = true
			break
		}
		if !commentIDPattern.MatchString(wire.ID) {
			return errors.New("a comment lacks a valid ID")
		}
		author, err := convertTeammate(wire.Author)
		if err != nil {
			return err
		}
		comment := Comment{ID: wire.ID, Author: author, IsPinned: wire.IsPinned, PostedAt: wire.PostedAt.instant}
		comment.Body, comment.IsBodyTruncated = truncateUTF8(wire.Body, MaxBodyBytes)
		output.Comments = append(output.Comments, comment)
	}
	return nil
}

func validateGetConversationInput(input GetConversationInput) error {
	if err := validateResourceID("conversationId", input.ConversationID, conversationIDPattern, "cnv_55c8c149"); err != nil {
		return err
	}
	switch {
	case input.MessageLimit < 0 || input.MessageLimit > MaxMessageLimit:
		return fmt.Errorf("messageLimit must be 1 to %d, or zero for %d", MaxMessageLimit, DefaultMessageLimit)
	case input.CommentLimit < 0 || input.CommentLimit > MaxCommentLimit:
		return fmt.Errorf("commentLimit must be 1 to %d, or zero for %d", MaxCommentLimit, DefaultCommentLimit)
	}
	return nil
}
