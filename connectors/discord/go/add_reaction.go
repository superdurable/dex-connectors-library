// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// maximumUnicodeEmojiBytes bounds a Unicode emoji, which may join several code points.
const maximumUnicodeEmojiBytes = 64

// customEmojiPattern matches a custom emoji as name:id, the form Discord's reaction route takes.
var customEmojiPattern = regexp.MustCompile(`^[A-Za-z0-9_]{2,32}:[0-9]{17,20}$`)

// AddReactionInput adds the bot's reaction to one message.
type AddReactionInput struct {
	// ChannelID is the channel's snowflake ID, 17 to 20 digits.
	ChannelID string `json:"channelId"`
	// MessageID is the message's snowflake ID.
	MessageID string `json:"messageId"`
	// Emoji is a Unicode emoji such as 👍, or a custom emoji as name:id, such as party_blob:123456789012345678.
	Emoji string `json:"emoji"`
}

// AddedReaction reports the reaction the bot now has on the message.
type AddedReaction struct {
	// ChannelID is the channel's snowflake ID.
	ChannelID string `json:"channelId"`
	// MessageID is the message's snowflake ID.
	MessageID string `json:"messageId"`
	// Emoji is the emoji as the input named it.
	Emoji string `json:"emoji"`
}

// AddReactionOperation implements the addReaction connector Mutation.
type AddReactionOperation struct{ client *Client }

var addReactionBranches = operationBranches{
	notFound: AddReactionBranchNotFound, rejected: AddReactionBranchProviderRejected, defect: AddReactionBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (AddReactionOperation) Definition() sdkgo.MutationDefinition { return AddReactionDefinition }

// IdempotencyKey derives the call's key from the stable connector call ID.
// Discord accepts no idempotency key, so it is not sent; Discord keeps one
// reaction per user and emoji, so a repeated PUT converges.
func (AddReactionOperation) IdempotencyKey(callID sdkgo.CallID, _ AddReactionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PUT and classifies its attempt. A request that may have
// been applied is retried, because repeating it is safe.
func (operation AddReactionOperation) Invoke(call sdkgo.Call, input AddReactionInput) sdkgo.MutationAttempt[AddedReaction] {
	if err := validateAddReactionInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddReactionBranchDefect, AddedReaction{}, providerFailurePointer("addReaction", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	path := "/channels/" + input.ChannelID + "/messages/" + input.MessageID + "/reactions/" + url.PathEscape(input.Emoji) + "/@me"
	_, outcome := operation.client.exchange(call, "addReaction", providerRequest{method: http.MethodPut, path: path}, input.MessageID)
	if outcome != nil {
		return mutationAttemptForOutcome[AddedReaction](*outcome, addReactionBranches)
	}
	added := AddedReaction{ChannelID: input.ChannelID, MessageID: input.MessageID, Emoji: input.Emoji}
	return sdkgo.NewMutationBranch(AddReactionBranchAdded, added, nil, operation.client.receipt(call, input.MessageID))
}

func validateAddReactionInput(input AddReactionInput) error {
	if err := validateSnowflake("channelId", input.ChannelID); err != nil {
		return err
	}
	if err := validateSnowflake("messageId", input.MessageID); err != nil {
		return err
	}
	if customEmojiPattern.MatchString(input.Emoji) || isUnicodeEmojiCandidate(input.Emoji) {
		return nil
	}
	return errors.New("emoji must be a Unicode emoji or a custom emoji as name:id")
}

// isUnicodeEmojiCandidate accepts a short non-ASCII symbol sequence; Discord itself rejects a non-emoji with Unknown Emoji.
func isUnicodeEmojiCandidate(value string) bool {
	if value == "" || len(value) > maximumUnicodeEmojiBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x80 && !unicode.IsDigit(character) && character != '#' && character != '*' {
			return false
		}
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return false
		}
	}
	return true
}
