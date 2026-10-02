// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const postChatMessageOperationID = "postChatMessage"

// PostChatMessageInput describes one message in an existing chat.
type PostChatMessageInput struct {
	// ChatID is the chat ID, such as 19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2 or a one-on-one chat's
	// 19:...@unq.gbl.spaces ID; the chatPicker unit saves it.
	ChatID string `json:"chatId"`
	// Content is the message body in ContentType.
	Content string `json:"content"`
	// ContentType is text or html; blank means text.
	ContentType ContentType `json:"contentType,omitempty"`
	// Importance is normal, high, or urgent; blank leaves Teams' default.
	Importance MessageImportance `json:"importance,omitempty"`
}

// PostChatMessageOperation implements the postChatMessage Mutation.
type PostChatMessageOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (PostChatMessageOperation) Definition() sdkgo.MutationDefinition {
	return PostChatMessageDefinition
}

// IdempotencyKey uses the stable connector Call ID. Graph accepts no idempotency key, so the key only
// correlates the Receipt; a Dex heartbeat checkpoint keeps the message to one send.
func (PostChatMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ PostChatMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /chats/{chat-id}/messages at most once per Step execution. Reading chat messages needs
// Chat.Read, which the connection does not request, so an unconfirmed send and an attempt that finds an
// earlier attempt's checkpoint select uncertain without another request.
func (operation PostChatMessageOperation) Invoke(call sdkgo.Call, input PostChatMessageInput) sdkgo.MutationAttempt[PostMessageOutput] {
	client := operation.client
	chatID := strings.TrimSpace(input.ChatID)
	requested := PostMessageOutput{ChatID: chatID}
	if !chatIDPattern.MatchString(chatID) {
		err := errors.New("chatId must be a chat ID such as 19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2")
		return sdkgo.NewMutationBranch(PostChatMessageBranchDefect, requested, failurePointer(postChatMessageOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	content, err := buildMessageContent(input.Content, input.ContentType, input.Importance, "", client.maxMessageBytes)
	if err != nil {
		return sdkgo.NewMutationBranch(PostChatMessageBranchDefect, requested, failurePointer(postChatMessageOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	return client.send(call, messagePost{
		operationID: postChatMessageOperationID, subject: "chat message",
		path: "/chats/" + chatID + "/messages", content: content, requested: requested,
		branches: postBranches{
			sent: PostChatMessageBranchSent, providerRejected: PostChatMessageBranchProviderRejected, defect: PostChatMessageBranchDefect,
		},
	})
}
