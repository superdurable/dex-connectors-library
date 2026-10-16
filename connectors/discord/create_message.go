// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaximumMessageCharacters is Discord's limit on message content length in Unicode characters.
	MaximumMessageCharacters = 2000
	// nonceLength is the 25-character maximum Discord accepts for a string nonce.
	nonceLength = 25
)

// CreateMessageInput sends one text message to a channel as the bot.
//
// Every mention is disabled, so text such as @everyone or <@id> renders but
// notifies nobody.
type CreateMessageInput struct {
	// ChannelID is the channel's snowflake ID, 17 to 20 digits.
	ChannelID string `json:"channelId"`
	// Content is the message text, 1 to 2000 characters and not only whitespace.
	Content string `json:"content"`
	// ReplyToMessageID makes the message a reply to this message in the same channel; blank sends a plain message.
	ReplyToMessageID string `json:"replyToMessageId,omitempty"`
}

// CreateMessageOperation implements the createMessage connector Mutation.
type CreateMessageOperation struct{ client *Client }

type createMessageRequestBody struct {
	Content          string                   `json:"content"`
	Nonce            string                   `json:"nonce"`
	EnforceNonce     bool                     `json:"enforce_nonce"`
	AllowedMentions  allowedMentions          `json:"allowed_mentions"`
	MessageReference *messageReferenceRequest `json:"message_reference,omitempty"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

type messageReferenceRequest struct {
	MessageID       string `json:"message_id"`
	FailIfNotExists bool   `json:"fail_if_not_exists"`
}

var createMessageBranches = operationBranches{
	notFound: CreateMessageBranchNotFound, rejected: CreateMessageBranchProviderRejected,
	invalidResponse: CreateMessageBranchInvalidResponse, defect: CreateMessageBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (CreateMessageOperation) Definition() sdkgo.MutationDefinition { return CreateMessageDefinition }

// IdempotencyKey derives the call's key from the stable connector call ID and input.
func (CreateMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one POST with a nonce derived from the idempotency key and
// enforce_nonce, then classifies the attempt. A request that may have been
// applied is retried, because Discord returns the earlier message for a
// repeated nonce within its enforcement window.
func (operation CreateMessageOperation) Invoke(call sdkgo.Call, input CreateMessageInput) sdkgo.MutationAttempt[Message] {
	requestBody, err := buildCreateMessageRequestBody(input, call.IdempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateMessageBranchDefect, Message{}, providerFailurePointer("createMessage", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateMessageBranchDefect, Message{}, providerFailurePointer("createMessage", sdkgo.FailureLocalDefect, "createMessage request could not be encoded"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "createMessage", providerRequest{
		method: http.MethodPost, path: "/channels/" + input.ChannelID + "/messages", body: encoded,
	}, input.ChannelID)
	if outcome != nil {
		return mutationAttemptForOutcome[Message](*outcome, createMessageBranches)
	}
	var decoded providerMessage
	message, err := Message{}, decodeResponse(response.body, &decoded)
	if err == nil {
		message, err = convertProviderMessage(decoded, input.ChannelID)
	}
	if err != nil {
		receipt := operation.client.receipt(call, input.ChannelID)
		return sdkgo.NewMutationBranch(CreateMessageBranchInvalidResponse, Message{}, providerFailurePointer("createMessage", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(CreateMessageBranchCreated, message, nil, operation.client.receipt(call, message.ID))
}

func buildCreateMessageRequestBody(input CreateMessageInput, idempotencyKey sdkgo.IdempotencyKey) (createMessageRequestBody, error) {
	if err := validateSnowflake("channelId", input.ChannelID); err != nil {
		return createMessageRequestBody{}, err
	}
	if strings.TrimSpace(input.Content) == "" {
		return createMessageRequestBody{}, errors.New("content must not be blank")
	}
	if !utf8.ValidString(input.Content) || utf8.RuneCountInString(input.Content) > MaximumMessageCharacters {
		return createMessageRequestBody{}, errors.New("content must be valid UTF-8 of at most 2000 characters")
	}
	if idempotencyKey == "" {
		return createMessageRequestBody{}, errors.New("createMessage requires an idempotency key")
	}
	body := createMessageRequestBody{
		Content: input.Content, Nonce: messageNonce(idempotencyKey), EnforceNonce: true,
		AllowedMentions: allowedMentions{Parse: []string{}},
	}
	if input.ReplyToMessageID != "" {
		if err := validateSnowflake("replyToMessageId", input.ReplyToMessageID); err != nil {
			return createMessageRequestBody{}, err
		}
		body.MessageReference = &messageReferenceRequest{MessageID: input.ReplyToMessageID, FailIfNotExists: true}
	}
	return body, nil
}

// messageNonce shortens the idempotency key to Discord's 25-character nonce
// limit with SHA-256 and URL-safe base64, about 150 bits of the digest.
func messageNonce(idempotencyKey sdkgo.IdempotencyKey) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return base64.RawURLEncoding.EncodeToString(digest[:])[:nonceLength]
}
