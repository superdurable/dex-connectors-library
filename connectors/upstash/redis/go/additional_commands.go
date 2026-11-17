// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// GetInfoInput requests the Redis server INFO report.
type GetInfoInput struct{}

// DatabaseInfo contains the Redis INFO report as returned by Upstash.
type DatabaseInfo struct {
	// Report is the textual Redis INFO response.
	Report string `json:"report"`
}

// GetInfoOperation reads Redis server information.
type GetInfoOperation struct{ client *Client }

// GetInfo binds a server information read to this client.
func (client *Client) GetInfo() GetInfoOperation { return GetInfoOperation{client} }

// Definition returns the generated Query contract.
func (GetInfoOperation) Definition() sdkgo.QueryDefinition { return GetInfoDefinition }

// Invoke reads the complete bounded Redis INFO report.
func (operation GetInfoOperation) Invoke(call sdkgo.Call, _ GetInfoInput) sdkgo.QueryAttempt[DatabaseInfo] {
	response := operation.client.executeRedisCommand(call, []any{"INFO"})
	if attempt, stopped := classifyRedisQuery[DatabaseInfo]("getInfo", response); stopped {
		return attempt
	}
	var report string
	if !decodeRedisResult(response.body, &report) {
		return redisQueryBranch[DatabaseInfo]("getInfo", "invalidResponse", sdkgo.FailureProtocol, "Upstash returned an invalid INFO report")
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("found"), DatabaseInfo{Report: report}, nil, sdkgo.Receipt{})
}

// PushListItemInput selects a list end and one value to append.
type PushListItemInput struct {
	// Key is the exact case-sensitive Redis list key.
	Key string `json:"key"`
	// Value is the string pushed onto the list, including an empty string.
	Value string `json:"value"`
	// Direction is left or right; blank chooses right.
	Direction string `json:"direction"`
}

// ListPush reports the list size after a push.
type ListPush struct {
	// Key is the requested list key.
	Key string `json:"key"`
	// Length is the resulting number of list elements.
	Length int64 `json:"length"`
}

// PushListItemOperation adds one string to a Redis list.
type PushListItemOperation struct{ client *Client }

// PushListItem binds a bounded-resend list push to this client.
func (client *Client) PushListItem() PushListItemOperation { return PushListItemOperation{client} }

// Definition returns the generated Mutation contract.
func (PushListItemOperation) Definition() sdkgo.MutationDefinition { return PushListItemDefinition }

// IdempotencyKey identifies retries of one list push.
func (PushListItemOperation) IdempotencyKey(callID sdkgo.CallID, _ PushListItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke pushes one value, allowing at most the configured number of possible sends.
func (operation PushListItemOperation) Invoke(call sdkgo.Call, input PushListItemInput) sdkgo.MutationAttempt[ListPush] {
	if !validRedisKey(input.Key) || !utf8.ValidString(input.Value) || (input.Direction != "" && input.Direction != "left" && input.Direction != "right") {
		return redisMutationBranch[ListPush]("pushListItem", "defect", sdkgo.FailureValidation, "Redis key, value, or direction is invalid")
	}
	command := "RPUSH"
	if input.Direction == "left" {
		command = "LPUSH"
	}
	return runLimitedRedisCommand(operation.client, call, "pushListItem", "pushed", []any{command, input.Key, input.Value}, func(raw json.RawMessage) (ListPush, bool) {
		var length int64
		if json.Unmarshal(raw, &length) != nil || length < 1 {
			return ListPush{}, false
		}
		return ListPush{Key: input.Key, Length: length}, true
	})
}

// PublishMessageInput identifies a channel and one message.
type PublishMessageInput struct {
	// Channel is the exact Redis channel name.
	Channel string `json:"channel"`
	// Message is the string delivered to current subscribers.
	Message string `json:"message"`
}

// MessagePublication reports the subscriber count at publication time.
type MessagePublication struct {
	// Channel is the requested channel name.
	Channel string `json:"channel"`
	// Subscribers is the number of recipients reported by Redis.
	Subscribers int64 `json:"subscribers"`
}

// PublishMessageOperation broadcasts one Redis channel message.
type PublishMessageOperation struct{ client *Client }

// PublishMessage binds a bounded-resend publication to this client.
func (client *Client) PublishMessage() PublishMessageOperation {
	return PublishMessageOperation{client}
}

// Definition returns the generated Mutation contract.
func (PublishMessageOperation) Definition() sdkgo.MutationDefinition { return PublishMessageDefinition }

// IdempotencyKey identifies retries of one publication.
func (PublishMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ PublishMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke publishes once per allowed send and reports the subscriber count.
func (operation PublishMessageOperation) Invoke(call sdkgo.Call, input PublishMessageInput) sdkgo.MutationAttempt[MessagePublication] {
	if !validRedisKey(input.Channel) || !utf8.ValidString(input.Message) {
		return redisMutationBranch[MessagePublication]("publishMessage", "defect", sdkgo.FailureValidation, "Redis channel or message is invalid")
	}
	return runLimitedRedisCommand(operation.client, call, "publishMessage", "published", []any{"PUBLISH", input.Channel, input.Message}, func(raw json.RawMessage) (MessagePublication, bool) {
		var subscribers int64
		if json.Unmarshal(raw, &subscribers) != nil || subscribers < 0 {
			return MessagePublication{}, false
		}
		return MessagePublication{Channel: input.Channel, Subscribers: subscribers}, true
	})
}

func runLimitedRedisCommand[T any](client *Client, call sdkgo.Call, operation, success string, arguments []any, decode func(json.RawMessage) (T, bool)) sdkgo.MutationAttempt[T] {
	command, err := json.Marshal(arguments)
	if err != nil {
		return redisMutationBranch[T](operation, "defect", sdkgo.FailureValidation, "Redis command is invalid")
	}
	fence, err := singledispatch.NewFence(call, 30_000_000_000, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return redisMutationBranch[T](operation, "defect", sdkgo.FailureLocalDefect, "Redis dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return redisMutationUncertain[T](operation)
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.runCommandWithContext(call, requestContext, command, true)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return redisMutationRetry[T](operation, sdkgo.FailureAvailability, 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return redisMutationRetry[T](operation, sdkgo.FailureAvailability, 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return redisMutationRetry[T](operation, kind, response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || err != nil {
		if canResend {
			return redisMutationRetry[T](operation, sdkgo.FailureAvailability, response.retryAfter)
		}
		return redisMutationUncertain[T](operation)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return redisMutationUncertain[T](operation)
		}
		return redisMutationBranch[T](operation, "providerRejected", sdkgo.FailureProviderRejection, "Upstash rejected the Redis command")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(response.body, &envelope) == nil && len(envelope) == 1 {
		if raw, exists := envelope["error"]; exists {
			var message string
			if json.Unmarshal(raw, &message) == nil && strings.TrimSpace(message) != "" {
				if decision.MaximumPriorSends > 0 {
					return redisMutationUncertain[T](operation)
				}
				return redisMutationBranch[T](operation, "providerRejected", sdkgo.FailureProviderRejection, "Upstash rejected the Redis command")
			}
		}
		if raw, exists := envelope["result"]; exists {
			if output, valid := decode(raw); valid {
				return sdkgo.NewMutationBranch(sdkgo.BranchID(success), output, nil, sdkgo.Receipt{})
			}
		}
	}
	return redisMutationUncertain[T](operation)
}

func redisMutationUncertain[T any](operation string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "upstash-redis", Operation: operation, Message: "Redis command may have taken effect but could not be confirmed"}
	return sdkgo.NewMutationUncertain(*new(T), failure, sdkgo.Receipt{})
}
