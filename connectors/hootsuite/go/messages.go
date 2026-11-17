// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetMessageInput identifies one outbound Hootsuite message.
type GetMessageInput struct {
	// ID is the decimal message ID returned by scheduling or listing.
	ID string `json:"id"`
}

// OutboundMessage identifies one Hootsuite message and its provider state.
type OutboundMessage struct {
	// ID is the stable Hootsuite message ID.
	ID string `json:"id"`
	// State is the outbound message state assigned by Hootsuite.
	State string `json:"state"`
	// SocialProfileID identifies the destination profile.
	SocialProfileID string `json:"socialProfileId"`
	// ScheduledSendTime is a UTC send time when the message was scheduled.
	ScheduledSendTime string `json:"scheduledSendTime,omitempty"`
}

// DeleteMessageInput identifies one message to delete.
type DeleteMessageInput struct {
	// ID is the decimal message ID returned by scheduling or listing.
	ID string `json:"id"`
}

// DeletedMessage identifies a message absent after deletion.
type DeletedMessage struct {
	// ID is the removed Hootsuite message ID.
	ID string `json:"id"`
}

// GetMessageOperation reads one outbound message.
type GetMessageOperation struct{ client *Client }

// GetMessage binds the message lookup Query to this client.
func (client *Client) GetMessage() GetMessageOperation { return GetMessageOperation{client} }

// Definition returns the generated message Query contract.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Invoke reads one message by its stable provider ID.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[OutboundMessage] {
	if !numericHootsuiteMessageID(input.ID) {
		return messageQueryBranch(GetMessageBranchDefect, sdkgo.FailureValidation, "message ID must be decimal")
	}
	response := operation.client.get(call, "/v1/messages/"+input.ID)
	if errors.Is(response.err, errCredential) {
		return messageQueryBranch(GetMessageBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return messageQueryRetry(sdkgo.FailureTransport, "Hootsuite request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return messageQueryRetry(sdkgo.FailureRateLimit, "Hootsuite rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return messageQueryRetry(sdkgo.FailureAvailability, "Hootsuite is temporarily unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return messageQueryBranch(GetMessageBranchNotFound, sdkgo.FailureProviderRejection, "Hootsuite message does not exist")
	}
	if response.status != http.StatusOK {
		return messageQueryBranch(GetMessageBranchProviderRejected, sdkgo.FailureProviderRejection, "Hootsuite rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return messageQueryBranch(GetMessageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Hootsuite response exceeded the configured limit")
	}
	if response.err != nil {
		return messageQueryRetry(sdkgo.FailureTransport, "Hootsuite response was interrupted", 0)
	}
	var wire struct {
		Data struct {
			ID            string `json:"id"`
			State         string `json:"state"`
			SocialProfile struct {
				ID string `json:"id"`
			} `json:"socialProfile"`
			ScheduledSendTime string `json:"scheduledSendTime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data.ID == "" || wire.Data.State == "" || wire.Data.SocialProfile.ID == "" || wire.Data.ID != input.ID {
		return messageQueryBranch(GetMessageBranchInvalidResponse, sdkgo.FailureProtocol, "Hootsuite returned unusable message metadata")
	}
	return sdkgo.NewQueryBranch(GetMessageBranchFound, OutboundMessage{ID: wire.Data.ID, State: wire.Data.State, SocialProfileID: wire.Data.SocialProfile.ID, ScheduledSendTime: wire.Data.ScheduledSendTime}, nil, sdkgo.Receipt{})
}

// DeleteMessageOperation removes an outbound message when its state permits.
type DeleteMessageOperation struct{ client *Client }

// DeleteMessage binds the message deletion Mutation to this client.
func (client *Client) DeleteMessage() DeleteMessageOperation { return DeleteMessageOperation{client} }

// Definition returns the generated message Mutation contract.
func (DeleteMessageOperation) Definition() sdkgo.MutationDefinition { return DeleteMessageDefinition }

// IdempotencyKey records the stable Dex call identity for this absolute deletion.
func (DeleteMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a message and safely repeats after unknown responses.
func (operation DeleteMessageOperation) Invoke(call sdkgo.Call, input DeleteMessageInput) sdkgo.MutationAttempt[DeletedMessage] {
	if !numericHootsuiteMessageID(input.ID) {
		return messageMutationBranch(DeleteMessageBranchDefect, sdkgo.FailureValidation, "message ID must be decimal")
	}
	response := operation.client.delete(call, "/v1/messages/"+input.ID)
	if errors.Is(response.err, errCredential) {
		return messageMutationBranch(DeleteMessageBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return messageMutationRetry(sdkgo.FailureTransport, "Hootsuite request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return messageMutationRetry(sdkgo.FailureRateLimit, "Hootsuite rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return messageMutationRetry(sdkgo.FailureAvailability, "Hootsuite is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusNotFound {
		return messageMutationBranch(DeleteMessageBranchProviderRejected, sdkgo.FailureProviderRejection, "Hootsuite rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(DeleteMessageBranchDeleted, DeletedMessage{ID: input.ID}, nil, sdkgo.Receipt{})
}

func numericHootsuiteMessageID(id string) bool {
	if id == "" || len(id) > 40 {
		return false
	}
	for _, digit := range id {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
func messageQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OutboundMessage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "getMessage", Message: message}
	return sdkgo.NewQueryBranch(branch, OutboundMessage{}, &failure, sdkgo.Receipt{})
}
func messageQueryRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[OutboundMessage] {
	return sdkgo.NewQueryRetry[OutboundMessage](sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "getMessage", Message: message}, delay)
}
func messageMutationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedMessage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "deleteMessage", Message: message}
	return sdkgo.NewMutationBranch(branch, DeletedMessage{}, &failure, sdkgo.Receipt{})
}
func messageMutationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedMessage] {
	return sdkgo.NewMutationRetry[DeletedMessage](sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "deleteMessage", Message: message}, delay)
}
