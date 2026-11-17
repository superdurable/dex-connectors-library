// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateGuildChannelInput creates one text channel in a Discord server.
type CreateGuildChannelInput struct {
	// GuildID is the server snowflake from Copy Server ID.
	GuildID string `json:"guildId"`
	// Name is the new channel name, 1 to 100 characters.
	Name string `json:"name"`
	// Topic is the optional text-channel topic, at most 1024 characters.
	Topic string `json:"topic,omitempty"`
}

// CreateGuildChannelOperation creates a text channel with bounded sends.
type CreateGuildChannelOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (CreateGuildChannelOperation) Definition() sdkgo.MutationDefinition {
	return CreateGuildChannelDefinition
}

// IdempotencyKey correlates the call; Discord has no channel creation deduplication key.
func (CreateGuildChannelOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateGuildChannelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a text channel, retrying unknown outcomes at most once.
func (operation CreateGuildChannelOperation) Invoke(call sdkgo.Call, input CreateGuildChannelInput) sdkgo.MutationAttempt[Channel] {
	if validateSnowflake("guildId", input.GuildID) != nil || strings.TrimSpace(input.Name) == "" || len([]rune(input.Name)) > 100 || !utf8.ValidString(input.Name) || len([]rune(input.Topic)) > 1024 || !utf8.ValidString(input.Topic) {
		return createChannelDefect("valid server ID, channel name, and topic are required")
	}
	fence, err := singledispatch.NewFence(call, CreateGuildChannelDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createChannelDefect("channel creation checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return operation.uncertainCreate(call, "channel creation checkpoint is unreadable")
	}
	if decision.Marker.ProviderID != "" {
		return operation.readCreatedChannel(call, input, decision.Marker.ProviderID)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return operation.uncertainCreate(call, "earlier channel creation may have applied without a confirmed ID")
	}
	credentials, credentialOutcome := operation.client.resolveCredentials(call, "createGuildChannel")
	if credentialOutcome != nil {
		if decision.Kind == singledispatch.SendAllowed {
			_ = fence.RecordNotSent()
		}
		return sdkgo.NewMutationBranch(CreateGuildChannelBranchDefect, Channel{}, &credentialOutcome.failure, sdkgo.Receipt{})
	}
	encoded, err := json.Marshal(struct {
		Name  string `json:"name"`
		Type  int    `json:"type"`
		Topic string `json:"topic,omitempty"`
	}{Name: input.Name, Type: 0, Topic: input.Topic})
	if err != nil {
		return createChannelDefect("channel request could not be encoded")
	}
	send := &createGuildChannelSend{client: operation.client, credentials: credentials, request: providerRequest{method: http.MethodPost, path: "/guilds/" + input.GuildID + "/channels", body: encoded}}
	if err := fence.Dispatch(send.execute); err != nil {
		return sdkgo.NewMutationRetry[Channel](providerFailure("createGuildChannel", sdkgo.FailureAvailability, "channel creation was not dispatched after checkpoint"), 0)
	}
	if errors.Is(send.err, errRequestNotBuilt) {
		_ = fence.RecordCanResend()
		return sdkgo.NewMutationRetry[Channel](providerFailure("createGuildChannel", sdkgo.FailureLocalDefect, "channel request could not be sent"), 0)
	}
	if send.err != nil {
		return operation.retryOrUncertain(call, decision, 0, "Discord channel creation outcome is unknown")
	}
	response := send.response
	if response.statusCode == http.StatusTooManyRequests || response.statusCode == http.StatusServiceUnavailable {
		_ = fence.RecordCanResend()
		outcome := classifyErrorResponse("createGuildChannel", response, operation.client.now())
		return sdkgo.NewMutationRetry[Channel](outcome.failure, outcome.retryAfter)
	}
	if response.statusCode == http.StatusRequestTimeout || response.statusCode >= 500 {
		outcome := classifyErrorResponse("createGuildChannel", response, operation.client.now())
		return operation.retryOrUncertain(call, decision, outcome.retryAfter, "Discord channel creation outcome is unknown")
	}
	if response.statusCode < 200 || response.statusCode >= 300 {
		if decision.Kind != singledispatch.SendAllowed {
			return operation.retryOrUncertain(call, decision, 0, "earlier channel creation may have applied before a later refusal")
		}
		outcome := classifyErrorResponse("createGuildChannel", response, operation.client.now())
		return sdkgo.NewMutationBranch(CreateGuildChannelBranchProviderRejected, Channel{}, &outcome.failure, operation.client.receipt(call, input.GuildID))
	}
	var decoded providerChannel
	if decodeResponse(response.body, &decoded) != nil {
		return operation.retryOrUncertain(call, decision, 0, "Discord accepted channel creation without a usable response")
	}
	if snowflakePattern.MatchString(decoded.ID) {
		_ = fence.RecordProviderID(decoded.ID)
	}
	channel, err := decodeChannelItem(decoded)
	if err != nil || channel.GuildID != input.GuildID || channel.Name != input.Name || channel.Type != 0 {
		if snowflakePattern.MatchString(decoded.ID) {
			return operation.readCreatedChannel(call, input, decoded.ID)
		}
		return operation.retryOrUncertain(call, decision, 0, "Discord accepted channel creation without a confirmed channel ID")
	}
	return sdkgo.NewMutationBranch(CreateGuildChannelBranchCreated, channel, nil, operation.client.receipt(call, channel.ID))
}

func (operation CreateGuildChannelOperation) readCreatedChannel(call sdkgo.Call, input CreateGuildChannelInput, channelID string) sdkgo.MutationAttempt[Channel] {
	response, outcome := operation.client.exchange(call, "createGuildChannel", providerRequest{method: http.MethodGet, path: "/channels/" + channelID}, channelID)
	if outcome != nil {
		if outcome.disposition == outcomeRetry {
			return sdkgo.NewMutationRetry[Channel](outcome.failure, outcome.retryAfter)
		}
		return operation.uncertainCreate(call, "created channel could not be read back")
	}
	channel, err := decodeChannel(response.body, channelID)
	if err != nil || channel.GuildID != input.GuildID || channel.Name != input.Name || channel.Type != 0 {
		return operation.uncertainCreate(call, "created channel identity differs from the request")
	}
	return sdkgo.NewMutationBranch(CreateGuildChannelBranchCreated, channel, nil, operation.client.receipt(call, channelID))
}

func (operation CreateGuildChannelOperation) retryOrUncertain(call sdkgo.Call, decision singledispatch.Decision, after time.Duration, message string) sdkgo.MutationAttempt[Channel] {
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return operation.uncertainCreate(call, message)
	}
	return sdkgo.NewMutationRetry[Channel](providerFailure("createGuildChannel", sdkgo.FailureAvailability, message), after)
}

func (CreateGuildChannelOperation) uncertainCreate(call sdkgo.Call, message string) sdkgo.MutationAttempt[Channel] {
	return sdkgo.NewMutationUncertain(Channel{}, providerFailure("createGuildChannel", sdkgo.FailureTransport, message), sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName})
}

func createChannelDefect(message string) sdkgo.MutationAttempt[Channel] {
	return sdkgo.NewMutationBranch(CreateGuildChannelBranchDefect, Channel{}, providerFailurePointer("createGuildChannel", sdkgo.FailureValidation, message), sdkgo.Receipt{})
}

type createGuildChannelSend struct {
	client      *Client
	credentials Credentials
	request     providerRequest
	response    providerResponse
	err         error
}

func (send *createGuildChannelSend) execute(ctx context.Context) error {
	send.response, send.err = send.client.sendWithContext(ctx, send.credentials, send.request)
	return nil
}
