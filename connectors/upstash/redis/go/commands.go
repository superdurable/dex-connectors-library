// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package redis

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SetKeyInput replaces a Redis string and optionally sets its absolute expiration.
type SetKeyInput struct {
	// Key is the exact case-sensitive Redis key.
	Key string `json:"key"`
	// Value is the complete replacement string, including an empty string.
	Value string `json:"value"`
	// ExpireAtUnixSeconds is a positive Unix time in seconds; zero keeps the key indefinitely.
	ExpireAtUnixSeconds int64 `json:"expireAtUnixSeconds"`
}

// KeyWrite identifies a key whose requested value was accepted.
type KeyWrite struct {
	// Key is the requested key.
	Key string `json:"key"`
}

// DeleteKeyInput identifies one key to remove.
type DeleteKeyInput struct {
	// Key is the exact case-sensitive Redis key.
	Key string `json:"key"`
}

// KeyDeletion reports whether a key existed when the delete ran.
type KeyDeletion struct {
	// Key is the requested key.
	Key string `json:"key"`
	// Existed is true when Redis removed an existing key.
	Existed bool `json:"existed"`
}

// ListKeysInput supplies a Redis glob pattern.
type ListKeysInput struct {
	// Pattern is a Redis glob pattern such as orders:*; blank matches all keys.
	Pattern string `json:"pattern"`
}

// KeyList holds matching key names.
type KeyList struct {
	// Keys contains Redis key names, without values.
	Keys []string `json:"keys"`
}

// GetListLengthInput identifies one Redis list.
type GetListLengthInput struct {
	// Key is the exact case-sensitive Redis list key.
	Key string `json:"key"`
}

// ListLength reports the size of a Redis list, including zero for an absent key.
type ListLength struct {
	// Key is the requested list key.
	Key string `json:"key"`
	// Length is the number of list elements.
	Length int64 `json:"length"`
}

// SetKeyOperation replaces a Redis string value.
type SetKeyOperation struct{ client *Client }

// SetKey binds a replacement write to this client.
func (client *Client) SetKey() SetKeyOperation { return SetKeyOperation{client} }

// Definition returns the generated Mutation contract.
func (SetKeyOperation) Definition() sdkgo.MutationDefinition { return SetKeyDefinition }

// IdempotencyKey derives the stable identity for this replacement.
func (SetKeyOperation) IdempotencyKey(callID sdkgo.CallID, _ SetKeyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the complete string, safely retrying an unknown outcome.
func (operation SetKeyOperation) Invoke(call sdkgo.Call, input SetKeyInput) sdkgo.MutationAttempt[KeyWrite] {
	if !validRedisKey(input.Key) || !utf8.ValidString(input.Value) || input.ExpireAtUnixSeconds < 0 {
		return redisMutationBranch[KeyWrite]("setKey", "defect", sdkgo.FailureValidation, "Redis key, value, or lifetime is invalid")
	}
	args := []any{"SET", input.Key, input.Value}
	if input.ExpireAtUnixSeconds > 0 {
		args = append(args, "EXAT", input.ExpireAtUnixSeconds)
	}
	response := operation.client.executeRedisCommand(call, args)
	if attempt, stopped := classifyRedisMutation[KeyWrite]("setKey", response); stopped {
		return attempt
	}
	var result string
	if !decodeRedisResult(response.body, &result) || result != "OK" {
		return redisMutationBranch[KeyWrite]("setKey", "invalidResponse", sdkgo.FailureProtocol, "Upstash returned an invalid SET result")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("set"), KeyWrite{Key: input.Key}, nil, sdkgo.Receipt{})
}

// DeleteKeyOperation removes a Redis key.
type DeleteKeyOperation struct{ client *Client }

// DeleteKey binds deletion to this client.
func (client *Client) DeleteKey() DeleteKeyOperation { return DeleteKeyOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteKeyOperation) Definition() sdkgo.MutationDefinition { return DeleteKeyDefinition }

// IdempotencyKey derives the stable identity for this deletion.
func (DeleteKeyOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteKeyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes the key and reports whether Redis removed a value.
func (operation DeleteKeyOperation) Invoke(call sdkgo.Call, input DeleteKeyInput) sdkgo.MutationAttempt[KeyDeletion] {
	if !validRedisKey(input.Key) {
		return redisMutationBranch[KeyDeletion]("deleteKey", "defect", sdkgo.FailureValidation, "Redis key is invalid")
	}
	response := operation.client.executeRedisCommand(call, []any{"DEL", input.Key})
	if attempt, stopped := classifyRedisMutation[KeyDeletion]("deleteKey", response); stopped {
		return attempt
	}
	var removed int64
	if !decodeRedisResult(response.body, &removed) || removed < 0 || removed > 1 {
		return redisMutationBranch[KeyDeletion]("deleteKey", "invalidResponse", sdkgo.FailureProtocol, "Upstash returned an invalid DEL result")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), KeyDeletion{Key: input.Key, Existed: removed == 1}, nil, sdkgo.Receipt{})
}

// ListKeysOperation finds Redis keys by glob pattern.
type ListKeysOperation struct{ client *Client }

// ListKeys binds key enumeration to this client.
func (client *Client) ListKeys() ListKeysOperation { return ListKeysOperation{client} }

// Definition returns the generated Query contract.
func (ListKeysOperation) Definition() sdkgo.QueryDefinition { return ListKeysDefinition }

// Invoke returns matching key names without reading their values.
func (operation ListKeysOperation) Invoke(call sdkgo.Call, input ListKeysInput) sdkgo.QueryAttempt[KeyList] {
	pattern := input.Pattern
	if pattern == "" {
		pattern = "*"
	}
	if len(pattern) > 512 || !utf8.ValidString(pattern) || strings.ContainsRune(pattern, '\x00') {
		return redisQueryBranch[KeyList]("listKeys", "defect", sdkgo.FailureValidation, "Redis pattern is invalid")
	}
	response := operation.client.executeRedisCommand(call, []any{"KEYS", pattern})
	if attempt, stopped := classifyRedisQuery[KeyList]("listKeys", response); stopped {
		return attempt
	}
	var keys []string
	if !decodeRedisResult(response.body, &keys) || keys == nil {
		return redisQueryBranch[KeyList]("listKeys", "invalidResponse", sdkgo.FailureProtocol, "Upstash returned an invalid KEYS result")
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("listed"), KeyList{Keys: keys}, nil, sdkgo.Receipt{})
}

// GetListLengthOperation reads the number of elements in a Redis list.
type GetListLengthOperation struct{ client *Client }

// GetListLength binds a list-size read to this client.
func (client *Client) GetListLength() GetListLengthOperation { return GetListLengthOperation{client} }

// Definition returns the generated Query contract.
func (GetListLengthOperation) Definition() sdkgo.QueryDefinition { return GetListLengthDefinition }

// Invoke reads a non-negative list length.
func (operation GetListLengthOperation) Invoke(call sdkgo.Call, input GetListLengthInput) sdkgo.QueryAttempt[ListLength] {
	if !validRedisKey(input.Key) {
		return redisQueryBranch[ListLength]("getListLength", "defect", sdkgo.FailureValidation, "Redis key is invalid")
	}
	response := operation.client.executeRedisCommand(call, []any{"LLEN", input.Key})
	if attempt, stopped := classifyRedisQuery[ListLength]("getListLength", response); stopped {
		return attempt
	}
	var length int64
	if !decodeRedisResult(response.body, &length) || length < 0 {
		return redisQueryBranch[ListLength]("getListLength", "invalidResponse", sdkgo.FailureProtocol, "Upstash returned an invalid LLEN result")
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("found"), ListLength{Key: input.Key, Length: length}, nil, sdkgo.Receipt{})
}

func validRedisKey(key string) bool {
	return key != "" && len(key) <= 512 && utf8.ValidString(key) && !strings.ContainsRune(key, '\x00')
}

func (client *Client) executeRedisCommand(call sdkgo.Call, arguments []any) providerResponse {
	command, err := json.Marshal(arguments)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	return client.runCommand(call, command)
}

func decodeRedisResult(body []byte, result any) bool {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil || len(envelope) != 1 {
		return false
	}
	raw, exists := envelope["result"]
	return exists && json.Unmarshal(raw, result) == nil
}

func classifyRedisQuery[T any](operation string, response providerResponse) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return redisQueryBranch[T](operation, "defect", sdkgo.FailureAuthentication, "Upstash token is unavailable"), true
	}
	if response.status == http.StatusTooManyRequests {
		return redisQueryRetry[T](operation, sdkgo.FailureRateLimit, response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return redisQueryRetry[T](operation, sdkgo.FailureAvailability, response.retryAfter), true
	}
	if errors.Is(response.err, errRequest) || (response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge)) {
		return redisQueryRetry[T](operation, sdkgo.FailureTransport, 0), true
	}
	if response.status != http.StatusOK {
		return redisQueryBranch[T](operation, "providerRejected", sdkgo.FailureProviderRejection, "Upstash rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return redisQueryBranch[T](operation, "invalidResponse", sdkgo.FailureResponseTooLarge, "Upstash response exceeded the configured limit"), true
	}
	if redisCommandRejected(response.body) {
		return redisQueryBranch[T](operation, "providerRejected", sdkgo.FailureProviderRejection, "Upstash rejected the Redis command"), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func classifyRedisMutation[T any](operation string, response providerResponse) (sdkgo.MutationAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return redisMutationBranch[T](operation, "defect", sdkgo.FailureAuthentication, "Upstash token is unavailable"), true
	}
	if response.status == http.StatusTooManyRequests {
		return redisMutationRetry[T](operation, sdkgo.FailureRateLimit, response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return redisMutationRetry[T](operation, sdkgo.FailureAvailability, response.retryAfter), true
	}
	if errors.Is(response.err, errRequest) || (response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge)) {
		return redisMutationRetry[T](operation, sdkgo.FailureTransport, 0), true
	}
	if response.status != http.StatusOK {
		return redisMutationBranch[T](operation, "providerRejected", sdkgo.FailureProviderRejection, "Upstash rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return redisMutationBranch[T](operation, "invalidResponse", sdkgo.FailureResponseTooLarge, "Upstash response exceeded the configured limit"), true
	}
	if redisCommandRejected(response.body) {
		return redisMutationBranch[T](operation, "providerRejected", sdkgo.FailureProviderRejection, "Upstash rejected the Redis command"), true
	}
	return sdkgo.MutationAttempt[T]{}, false
}

func redisCommandRejected(body []byte) bool {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil || len(envelope) != 1 { return false }
	raw, exists := envelope["error"]
	if !exists { return false }
	var message string
	return json.Unmarshal(raw, &message) == nil && strings.TrimSpace(message) != ""
}

func redisQueryRetry[T any](operation string, kind sdkgo.FailureKind, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "upstash-redis", Operation: operation, Message: "Upstash command could not be completed"}, delay)
}

func redisMutationRetry[T any](operation string, kind sdkgo.FailureKind, delay time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "upstash-redis", Operation: operation, Message: "Upstash command could not be completed"}, delay)
}

func redisQueryBranch[T any](operation, branch string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "upstash-redis", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(sdkgo.BranchID(branch), *new(T), &failure, sdkgo.Receipt{})
}

func redisMutationBranch[T any](operation, branch string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "upstash-redis", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(sdkgo.BranchID(branch), *new(T), &failure, sdkgo.Receipt{})
}
