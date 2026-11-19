// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// MessageReferenceInput identifies one message in one chat.
type MessageReferenceInput struct {
	// ChatID is a decimal chat ID or public @username.
	ChatID string `json:"chatId"`
	// MessageID is the positive Telegram message ID within the chat.
	MessageID int64 `json:"messageId"`
}

// EditMessageTextInput replaces plain text on one bot-editable chat message.
type EditMessageTextInput struct {
	// ChatID is a decimal chat ID or public @username.
	ChatID string `json:"chatId"`
	// MessageID is the positive Telegram message ID within the chat.
	MessageID int64 `json:"messageId"`
	// Text is 1 through 4096 UTF-8 characters.
	Text string `json:"text"`
}

// SendChatActionInput broadcasts a transient bot activity.
type SendChatActionInput struct {
	// ChatID is a decimal chat ID or public @username.
	ChatID string `json:"chatId"`
	// Action is a documented Telegram activity such as typing.
	Action string `json:"action"`
}

// MessageActionResult identifies a confirmed message action without message content.
type MessageActionResult struct {
	// ChatID is the requested chat ID or @username.
	ChatID string `json:"chatId"`
	// MessageID is the selected message ID.
	MessageID int64 `json:"messageId"`
}

// EditMessageTextOperation replaces message text.
type EditMessageTextOperation struct{ client *Client }

// EditMessageText binds the mutation to this client.
func (client *Client) EditMessageText() EditMessageTextOperation {
	return EditMessageTextOperation{client}
}

// Definition returns the generated mutation contract.
func (EditMessageTextOperation) Definition() sdkgo.MutationDefinition {
	return EditMessageTextDefinition
}

// IdempotencyKey derives stable Dex identity for absolute text replacement.
func (EditMessageTextOperation) IdempotencyKey(callID sdkgo.CallID, _ EditMessageTextInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces plain text and accepts Telegram's not-modified response as confirmation.
func (operation EditMessageTextOperation) Invoke(call sdkgo.Call, input EditMessageTextInput) sdkgo.MutationAttempt[MessageActionResult] {
	if !validChatID(input.ChatID) || input.MessageID <= 0 || !utf8.ValidString(input.Text) || strings.TrimSpace(input.Text) == "" || utf8.RuneCountInString(input.Text) > 4096 {
		return chatMutationBranch(EditMessageTextBranchDefect, MessageActionResult{}, "editMessageText", sdkgo.FailureValidation, "message target or text invalid")
	}
	parameters := url.Values{"chat_id": {input.ChatID}, "message_id": {strconv.FormatInt(input.MessageID, 10)}, "text": {input.Text}}
	return invokeMessageAction(call, operation.client, "editMessageText", parameters, input.ChatID, input.MessageID, EditMessageTextBranchEdited, EditMessageTextBranchProviderRejected, EditMessageTextBranchInvalidResponse, EditMessageTextBranchDefect, "message is not modified", true)
}

// DeleteMessageOperation deletes one Telegram message.
type DeleteMessageOperation struct{ client *Client }

// DeleteMessage binds the mutation to this client.
func (client *Client) DeleteMessage() DeleteMessageOperation { return DeleteMessageOperation{client} }

// Definition returns the generated mutation contract.
func (DeleteMessageOperation) Definition() sdkgo.MutationDefinition { return DeleteMessageDefinition }

// IdempotencyKey derives stable Dex identity for a repeatable delete.
func (DeleteMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ MessageReferenceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a message and accepts a definitive already-absent response.
func (operation DeleteMessageOperation) Invoke(call sdkgo.Call, input MessageReferenceInput) sdkgo.MutationAttempt[MessageActionResult] {
	if !validMessageReference(input) {
		return chatMutationBranch(DeleteMessageBranchDefect, MessageActionResult{}, "deleteMessage", sdkgo.FailureValidation, "message target invalid")
	}
	return invokeMessageAction(call, operation.client, "deleteMessage", messageReferenceParameters(input), input.ChatID, input.MessageID, DeleteMessageBranchDeleted, DeleteMessageBranchProviderRejected, DeleteMessageBranchInvalidResponse, DeleteMessageBranchDefect, "message to delete not found", false)
}

// PinChatMessageOperation pins one message.
type PinChatMessageOperation struct{ client *Client }

// PinChatMessage binds the mutation to this client.
func (client *Client) PinChatMessage() PinChatMessageOperation {
	return PinChatMessageOperation{client}
}

// Definition returns the generated mutation contract.
func (PinChatMessageOperation) Definition() sdkgo.MutationDefinition { return PinChatMessageDefinition }

// IdempotencyKey derives stable Dex identity for an absolute pin action.
func (PinChatMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ MessageReferenceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke pins a selected message.
func (operation PinChatMessageOperation) Invoke(call sdkgo.Call, input MessageReferenceInput) sdkgo.MutationAttempt[MessageActionResult] {
	if !validMessageReference(input) {
		return chatMutationBranch(PinChatMessageBranchDefect, MessageActionResult{}, "pinChatMessage", sdkgo.FailureValidation, "message target invalid")
	}
	return invokeMessageAction(call, operation.client, "pinChatMessage", messageReferenceParameters(input), input.ChatID, input.MessageID, PinChatMessageBranchPinned, PinChatMessageBranchProviderRejected, PinChatMessageBranchInvalidResponse, PinChatMessageBranchDefect, "already pinned", false)
}

// UnpinChatMessageOperation unpins one selected message.
type UnpinChatMessageOperation struct{ client *Client }

// UnpinChatMessage binds the mutation to this client.
func (client *Client) UnpinChatMessage() UnpinChatMessageOperation {
	return UnpinChatMessageOperation{client}
}

// Definition returns the generated mutation contract.
func (UnpinChatMessageOperation) Definition() sdkgo.MutationDefinition {
	return UnpinChatMessageDefinition
}

// IdempotencyKey derives stable Dex identity for an absolute unpin action.
func (UnpinChatMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ MessageReferenceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one selected message from the pinned set.
func (operation UnpinChatMessageOperation) Invoke(call sdkgo.Call, input MessageReferenceInput) sdkgo.MutationAttempt[MessageActionResult] {
	if !validMessageReference(input) {
		return chatMutationBranch(UnpinChatMessageBranchDefect, MessageActionResult{}, "unpinChatMessage", sdkgo.FailureValidation, "message target invalid")
	}
	return invokeMessageAction(call, operation.client, "unpinChatMessage", messageReferenceParameters(input), input.ChatID, input.MessageID, UnpinChatMessageBranchUnpinned, UnpinChatMessageBranchProviderRejected, UnpinChatMessageBranchInvalidResponse, UnpinChatMessageBranchDefect, "not pinned", false)
}

// SendChatActionOperation displays transient bot activity in a chat.
type SendChatActionOperation struct{ client *Client }

// SendChatAction binds the mutation to this client.
func (client *Client) SendChatAction() SendChatActionOperation {
	return SendChatActionOperation{client}
}

// Definition returns the generated mutation contract.
func (SendChatActionOperation) Definition() sdkgo.MutationDefinition { return SendChatActionDefinition }

// IdempotencyKey derives stable Dex identity for a harmless transient action.
func (SendChatActionOperation) IdempotencyKey(callID sdkgo.CallID, _ SendChatActionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke displays a Telegram activity for at most five seconds.
func (operation SendChatActionOperation) Invoke(call sdkgo.Call, input SendChatActionInput) sdkgo.MutationAttempt[ChatActionResult] {
	if !validChatID(input.ChatID) || !validChatAction(input.Action) {
		return chatMutationBranch(SendChatActionBranchDefect, ChatActionResult{}, "sendChatAction", sdkgo.FailureValidation, "chat or action invalid")
	}
	return invokeChatAction(call, operation.client, "sendChatAction", url.Values{"chat_id": {input.ChatID}, "action": {input.Action}}, input.ChatID, SendChatActionBranchSent, SendChatActionBranchProviderRejected, SendChatActionBranchInvalidResponse, SendChatActionBranchDefect)
}

func validMessageReference(input MessageReferenceInput) bool {
	return validChatID(input.ChatID) && input.MessageID > 0
}

func messageReferenceParameters(input MessageReferenceInput) url.Values {
	return url.Values{"chat_id": {input.ChatID}, "message_id": {strconv.FormatInt(input.MessageID, 10)}}
}

func validChatAction(action string) bool {
	switch action {
	case "typing", "upload_photo", "record_video", "upload_video", "record_voice", "upload_voice", "upload_document", "choose_sticker", "find_location", "record_video_note", "upload_video_note":
		return true
	}
	return false
}

func invokeMessageAction(call sdkgo.Call, client *Client, operation string, parameters url.Values, chatID string, messageID int64, success, rejected, invalid, defect sdkgo.BranchID, alreadyDoneDescription string, expectsMessage bool) sdkgo.MutationAttempt[MessageActionResult] {
	response := client.callBot(call, http.MethodPost, operation, parameters)
	if retry, failure := botRetry(operation, response); retry {
		return sdkgo.NewMutationRetry[MessageActionResult](failure, response.retryAfter)
	}
	if response.err == errBotCredential {
		return chatMutationBranch(defect, MessageActionResult{}, operation, sdkgo.FailureAuthentication, "bot token unavailable")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return chatMutationBranch(invalid, MessageActionResult{}, operation, sdkgo.FailureResponseTooLarge, "Bot API response exceeded limit")
	}
	envelope, err := decodeBotEnvelope(response.body)
	if err != nil {
		return chatMutationBranch(invalid, MessageActionResult{}, operation, sdkgo.FailureProtocol, "Bot API response invalid")
	}
	if !envelope.OK {
		if response.status == http.StatusBadRequest && strings.Contains(strings.ToLower(envelope.Description), alreadyDoneDescription) {
			return sdkgo.NewMutationBranch(success, MessageActionResult{ChatID: chatID, MessageID: messageID}, nil, sdkgo.Receipt{})
		}
		return chatMutationBranch(rejected, MessageActionResult{}, operation, botRejectionKind(response.status), "Telegram rejected message action")
	}
	if response.status != http.StatusOK {
		return chatMutationBranch(invalid, MessageActionResult{}, operation, sdkgo.FailureProtocol, "Bot API status inconsistent")
	}
	if expectsMessage {
		var result struct {
			MessageID int64 `json:"message_id"`
		}
		if json.Unmarshal(envelope.Result, &result) != nil || result.MessageID != messageID {
			return chatMutationBranch(invalid, MessageActionResult{}, operation, sdkgo.FailureProtocol, "edited message identity invalid")
		}
	} else {
		var confirmed bool
		if json.Unmarshal(envelope.Result, &confirmed) != nil || !confirmed {
			return chatMutationBranch(invalid, MessageActionResult{}, operation, sdkgo.FailureProtocol, "Bot API confirmation invalid")
		}
	}
	return sdkgo.NewMutationBranch(success, MessageActionResult{ChatID: chatID, MessageID: messageID}, nil, sdkgo.Receipt{})
}
