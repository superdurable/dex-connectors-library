// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const postChannelMessageOperationID = "postChannelMessage"

// PostChannelMessageInput describes one top-level channel message.
type PostChannelMessageInput struct {
	// TeamID is the team's GUID, such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b; the teamPicker unit saves it.
	TeamID string `json:"teamId"`
	// ChannelID is the channel ID, such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2; the
	// channelPicker unit saves it.
	ChannelID string `json:"channelId"`
	// Subject is an optional one-line plain-text subject Teams shows above the message.
	Subject string `json:"subject,omitempty"`
	// Content is the message body in ContentType.
	Content string `json:"content"`
	// ContentType is text or html; blank means text.
	ContentType ContentType `json:"contentType,omitempty"`
	// Importance is normal, high, or urgent; blank leaves Teams' default.
	Importance MessageImportance `json:"importance,omitempty"`
}

// PostChannelMessageOperation implements the postChannelMessage Mutation.
type PostChannelMessageOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (PostChannelMessageOperation) Definition() sdkgo.MutationDefinition {
	return PostChannelMessageDefinition
}

// IdempotencyKey uses the stable connector Call ID. Graph accepts no idempotency key, so the key only
// correlates the Receipt; a Dex heartbeat checkpoint keeps the message to one send.
func (PostChannelMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ PostChannelMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /teams/{team-id}/channels/{channel-id}/messages at most once per Step execution. An
// attempt that finds an earlier attempt's checkpoint reads the channel's 50 most recently active root
// messages and reports the one with the same subject and text, created by a person after the first send,
// as sent; otherwise it selects uncertain. Reading back needs ChannelMessage.Read.All.
func (operation PostChannelMessageOperation) Invoke(call sdkgo.Call, input PostChannelMessageInput) sdkgo.MutationAttempt[PostMessageOutput] {
	client := operation.client
	teamID, channelID := strings.TrimSpace(input.TeamID), strings.TrimSpace(input.ChannelID)
	requested := PostMessageOutput{TeamID: teamID, ChannelID: channelID}
	if err := validateTeamAndChannel(teamID, channelID); err != nil {
		return sdkgo.NewMutationBranch(PostChannelMessageBranchDefect, requested, failurePointer(postChannelMessageOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	content, err := buildMessageContent(input.Content, input.ContentType, input.Importance, strings.TrimSpace(input.Subject), client.maxMessageBytes)
	if err != nil {
		return sdkgo.NewMutationBranch(PostChannelMessageBranchDefect, requested, failurePointer(postChannelMessageOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	return client.send(call, messagePost{
		operationID: postChannelMessageOperationID, subject: "channel message",
		path: channelMessagesPath(teamID, channelID), content: content, requested: requested,
		branches: postBranches{
			sent: PostChannelMessageBranchSent, providerRejected: PostChannelMessageBranchProviderRejected, defect: PostChannelMessageBranchDefect,
		},
		readBackPath: channelMessagesPath(teamID, channelID),
	})
}
