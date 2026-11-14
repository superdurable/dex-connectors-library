// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meiqia

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetConversationInput identifies one Meiqia conversation.
type GetConversationInput struct {
	// ConversationID is a positive provider conversation identifier.
	ConversationID int64 `json:"conversationID"`
}

// GetConversationOutput contains only non-content conversation metadata.
type GetConversationOutput struct {
	// ConversationID is the provider conversation identifier.
	ConversationID int64 `json:"conversationID"`
	// StartedAt is the provider's conversation start time.
	StartedAt string `json:"startedAt"`
	// EndedAt is the provider's conversation end time when available.
	EndedAt string `json:"endedAt"`
	// MainChannel is the provider's channel name when available.
	MainChannel string `json:"mainChannel"`
}

// GetConversationOperation reads one conversation without changing provider state.
type GetConversationOperation struct{ client *Client }

// Definition returns the generated conversation read contract.
func (GetConversationOperation) Definition() sdkgo.QueryDefinition { return GetConversationDefinition }

// Invoke reads conversation metadata and discards message and customer data.
func (operation GetConversationOperation) Invoke(call sdkgo.Call, input GetConversationInput) sdkgo.QueryAttempt[GetConversationOutput] {
	if input.ConversationID <= 0 {
		return conversationBranch(GetConversationBranchDefect, sdkgo.FailureValidation, "Meiqia requires a positive conversation ID")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.AccessToken.Reveal()) {
		return conversationBranch(GetConversationBranchDefect, sdkgo.FailureAuthentication, "Meiqia Access Token is unavailable or invalid")
	}
	endpoint := conversationEndpoint + strconv.FormatInt(input.ConversationID, 10)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return conversationBranch(GetConversationBranchDefect, sdkgo.FailureValidation, "Meiqia conversation request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return conversationRetry(sdkgo.FailureAvailability, "Meiqia conversation request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return conversationRetry(sdkgo.FailureRateLimit, "Meiqia rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return conversationRetry(sdkgo.FailureAvailability, "Meiqia conversation service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return conversationBranch(GetConversationBranchNoData, sdkgo.FailureNotFound, "Meiqia did not find the conversation")
	}
	if response.StatusCode != http.StatusOK {
		return classifyConversationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return conversationBranch(GetConversationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Meiqia response exceeded the configured limit")
	}
	if err != nil {
		return conversationRetry(sdkgo.FailureAvailability, "Meiqia response was interrupted", 0)
	}
	var wire struct {
		ConversationID int64  `json:"conv_id"`
		StartedAt      string `json:"conv_start_tm"`
		EndedAt        string `json:"conv_end_tm"`
		MainChannel    string `json:"main_channel"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.ConversationID != input.ConversationID || wire.StartedAt == "" || len(wire.StartedAt) > 64 || len(wire.EndedAt) > 64 || len(wire.MainChannel) > 64 {
		return conversationBranch(GetConversationBranchInvalidResponse, sdkgo.FailureProtocol, "Meiqia returned invalid conversation data")
	}
	output := GetConversationOutput{ConversationID: wire.ConversationID, StartedAt: wire.StartedAt, EndedAt: wire.EndedAt, MainChannel: wire.MainChannel}
	return sdkgo.NewQueryBranch(GetConversationBranchFound, output, nil, sdkgo.Receipt{})
}

func classifyConversationHTTPStatus(status int) sdkgo.QueryAttempt[GetConversationOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return conversationBranch(GetConversationBranchProviderRejected, kind, "Meiqia rejected conversation request with HTTP "+strconv.Itoa(status))
}
func conversationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetConversationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "meiqia", Operation: "getConversation", Message: message}
	return sdkgo.NewQueryBranch(branch, GetConversationOutput{}, &failure, sdkgo.Receipt{})
}
func conversationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetConversationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "meiqia", Operation: "getConversation", Message: message}
	return sdkgo.NewQueryRetry[GetConversationOutput](failure, delay)
}
