// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const postThreadReplyOperationID = "postThreadReply"

// PostThreadReplyInput describes one reply to an existing channel message.
type PostThreadReplyInput struct {
	// TeamID is the team's GUID, such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b.
	TeamID string `json:"teamId"`
	// ChannelID is the channel ID, such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2.
	ChannelID string `json:"channelId"`
	// MessageID is the root message's ID, such as the MessageID a postChannelMessage Result returned.
	MessageID string `json:"messageId"`
	// Content is the reply body in ContentType.
	Content string `json:"content"`
	// ContentType is text or html; blank means text.
	ContentType ContentType `json:"contentType,omitempty"`
	// Importance is normal, high, or urgent; blank leaves Teams' default.
	Importance MessageImportance `json:"importance,omitempty"`
}

// PostThreadReplyOperation implements the postThreadReply Mutation.
type PostThreadReplyOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (PostThreadReplyOperation) Definition() sdkgo.MutationDefinition {
	return PostThreadReplyDefinition
}

// IdempotencyKey uses the stable connector Call ID. Graph accepts no idempotency key, so the key only
// correlates the Receipt; a Dex heartbeat checkpoint keeps the reply to one send.
func (PostThreadReplyOperation) IdempotencyKey(callID sdkgo.CallID, _ PostThreadReplyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /teams/{team-id}/channels/{channel-id}/messages/{message-id}/replies at most once per
// Step execution. An attempt that finds an earlier attempt's checkpoint reads the thread's 50 newest replies
// and reports the one with the same text, created by a person after the first send, as sent; otherwise it
// selects uncertain. Reading back needs ChannelMessage.Read.All.
func (operation PostThreadReplyOperation) Invoke(call sdkgo.Call, input PostThreadReplyInput) sdkgo.MutationAttempt[PostMessageOutput] {
	client := operation.client
	teamID, channelID, messageID := strings.TrimSpace(input.TeamID), strings.TrimSpace(input.ChannelID), strings.TrimSpace(input.MessageID)
	requested := PostMessageOutput{TeamID: teamID, ChannelID: channelID, ReplyToID: messageID}
	if err := validateTeamAndChannel(teamID, channelID); err != nil {
		return sdkgo.NewMutationBranch(PostThreadReplyBranchDefect, requested, failurePointer(postThreadReplyOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	if err := validateMessageID("messageId", messageID); err != nil {
		return sdkgo.NewMutationBranch(PostThreadReplyBranchDefect, requested, failurePointer(postThreadReplyOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	content, err := buildMessageContent(input.Content, input.ContentType, input.Importance, "", client.maxMessageBytes)
	if err != nil {
		return sdkgo.NewMutationBranch(PostThreadReplyBranchDefect, requested, failurePointer(postThreadReplyOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	repliesPath := threadRepliesPath(teamID, channelID, messageID)
	return client.send(call, messagePost{
		operationID: postThreadReplyOperationID, subject: "thread reply",
		path: repliesPath, content: content, requested: requested,
		branches: postBranches{
			sent: PostThreadReplyBranchSent, providerRejected: PostThreadReplyBranchProviderRejected, defect: PostThreadReplyBranchDefect,
		},
		readBackPath: repliesPath, readBackReplyToID: messageID,
	})
}
