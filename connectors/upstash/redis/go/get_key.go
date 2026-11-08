// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package redis

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetKeyInput identifies one Redis string key.
type GetKeyInput struct {
	// Key is the exact case-sensitive Redis key.
	Key string `json:"key"`
}

// KeyLookup contains the requested key and its string value when present.
type KeyLookup struct {
	// Key is the requested Redis key.
	Key string `json:"key"`
	// Value is empty when the key is missing or stores an empty string.
	Value string `json:"value"`
	// Exists distinguishes a missing key from an empty string value.
	Exists bool `json:"exists"`
}

// GetKeyOperation reads one Redis string value.
type GetKeyOperation struct{ client *Client }

// GetKey binds the Query to this client.
func (client *Client) GetKey() GetKeyOperation { return GetKeyOperation{client} }

// Definition returns the generated Query contract.
func (GetKeyOperation) Definition() sdkgo.QueryDefinition { return GetKeyDefinition }

// Invoke executes one Upstash GET command and classifies its outcome.
func (operation GetKeyOperation) Invoke(call sdkgo.Call, input GetKeyInput) sdkgo.QueryAttempt[KeyLookup] {
	if input.Key == "" || len(input.Key) > 512 || !utf8.ValidString(input.Key) || strings.ContainsRune(input.Key, '\x00') {
		return keyBranch(GetKeyBranchDefect, KeyLookup{}, sdkgo.FailureValidation, "Redis key is invalid")
	}
	command, _ := json.Marshal([]string{"GET", input.Key})
	response := operation.client.readKey(call, command)
	if errors.Is(response.err, errCredential) {
		return keyBranch(GetKeyBranchDefect, KeyLookup{}, sdkgo.FailureAuthentication, "Upstash token is unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[KeyLookup](keyFailure(sdkgo.FailureTransport, "Upstash request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[KeyLookup](keyFailure(sdkgo.FailureRateLimit, "Upstash rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[KeyLookup](keyFailure(sdkgo.FailureAvailability, "Upstash is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return keyBranch(GetKeyBranchProviderRejected, KeyLookup{}, sdkgo.FailureProviderRejection, "Upstash rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return keyBranch(GetKeyBranchInvalidResponse, KeyLookup{}, sdkgo.FailureResponseTooLarge, "Upstash response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[KeyLookup](keyFailure(sdkgo.FailureTransport, "Upstash response was interrupted"), 0)
	}
	var envelope struct {
		Result *string `json:"result"`
		Error  *string `json:"error"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		return keyBranch(GetKeyBranchInvalidResponse, KeyLookup{}, sdkgo.FailureProtocol, "Upstash returned invalid JSON")
	}
	if envelope.Error != nil {
		return keyBranch(GetKeyBranchProviderRejected, KeyLookup{}, sdkgo.FailureProviderRejection, "Upstash rejected the GET command")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.body, &fields); err != nil || fields == nil || len(fields) != 1 {
		return keyBranch(GetKeyBranchInvalidResponse, KeyLookup{}, sdkgo.FailureProtocol, "Upstash returned invalid JSON")
	}
	if _, exists := fields["result"]; !exists {
		return keyBranch(GetKeyBranchInvalidResponse, KeyLookup{}, sdkgo.FailureProtocol, "Upstash returned invalid JSON")
	}
	if envelope.Result == nil {
		return sdkgo.NewQueryBranch(GetKeyBranchMissing, KeyLookup{Key: input.Key}, nil, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetKeyBranchFound, KeyLookup{Key: input.Key, Value: *envelope.Result, Exists: true}, nil, sdkgo.Receipt{})
}
func keyFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "upstash-redis", Operation: "getKey", Message: message}
}
func keyBranch(branch sdkgo.BranchID, value KeyLookup, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[KeyLookup] {
	failure := keyFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
