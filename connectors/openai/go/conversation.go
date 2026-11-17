// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ConversationMessage is one initial message in a new conversation.
type ConversationMessage struct {
	// Type is always message.
	Type string `json:"type"`
	// Role is user, assistant, system, or developer.
	Role string `json:"role"`
	// Content is the text of the message.
	Content string `json:"content"`
}

// CreateConversationRequest supplies initial messages and up to sixteen metadata pairs.
type CreateConversationRequest struct {
	// Items are messages to store when creating the conversation.
	Items []ConversationMessage `json:"items,omitempty"`
	// Metadata is a set of non-secret labels used to organize conversations.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ConversationIDRequest identifies a conversation.
type ConversationIDRequest struct {
	// ConversationID is the ID returned by createConversation or shown in the OpenAI Platform.
	ConversationID string `json:"conversationId"`
}

// UpdateConversationRequest replaces the metadata map of a conversation.
type UpdateConversationRequest struct {
	// ConversationID identifies the conversation.
	ConversationID string `json:"conversationId"`
	// Metadata is the complete replacement set of up to sixteen non-secret labels.
	Metadata map[string]string `json:"metadata"`
}

// Conversation is the stable identity and metadata returned by OpenAI.
type Conversation struct {
	// ID is the provider conversation ID.
	ID string `json:"id"`
	// CreatedAt is the Unix timestamp in seconds.
	CreatedAt int64 `json:"createdAt"`
	// Metadata is the provider's stored label map.
	Metadata map[string]string `json:"metadata"`
}

// CreateConversationOperation creates a conversation.
type CreateConversationOperation struct{ client *Client }

// RetrieveConversationOperation reads a conversation.
type RetrieveConversationOperation struct{ client *Client }

// UpdateConversationOperation replaces conversation metadata.
type UpdateConversationOperation struct{ client *Client }

// DeleteConversationOperation removes a conversation.
type DeleteConversationOperation struct{ client *Client }

// CreateConversation returns the bound create Mutation.
func (client *Client) CreateConversation() CreateConversationOperation {
	return CreateConversationOperation{client}
}

// RetrieveConversation returns the bound retrieve Query.
func (client *Client) RetrieveConversation() RetrieveConversationOperation {
	return RetrieveConversationOperation{client}
}

// UpdateConversation returns the bound metadata replacement Mutation.
func (client *Client) UpdateConversation() UpdateConversationOperation {
	return UpdateConversationOperation{client}
}

// DeleteConversation returns the bound delete Mutation.
func (client *Client) DeleteConversation() DeleteConversationOperation {
	return DeleteConversationOperation{client}
}

// Definition returns the generated create contract.
func (CreateConversationOperation) Definition() sdkgo.MutationDefinition {
	return CreateConversationDefinition
}

// Definition returns the generated retrieve contract.
func (RetrieveConversationOperation) Definition() sdkgo.QueryDefinition {
	return RetrieveConversationDefinition
}

// Definition returns the generated update contract.
func (UpdateConversationOperation) Definition() sdkgo.MutationDefinition {
	return UpdateConversationDefinition
}

// Definition returns the generated delete contract.
func (DeleteConversationOperation) Definition() sdkgo.MutationDefinition {
	return DeleteConversationDefinition
}

// IdempotencyKey uses the stable call ID for correlation; OpenAI does not document create deduplication.
func (CreateConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateConversationRequest) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the stable call ID for metadata replacement.
func (UpdateConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateConversationRequest) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the stable call ID for deletion.
func (DeleteConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ ConversationIDRequest) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

type conversationExchange struct {
	status         int
	body           []byte
	header         http.Header
	requestID      string
	err            error
	requestFailure *sdkgo.Failure
}

func (client *Client) exchangeConversation(call sdkgo.Call, ctx context.Context, method, path string, payload any) conversationExchange {
	request, failure := client.newRequest(call, method, path, "", payload)
	if failure != nil {
		return conversationExchange{requestFailure: &failure.failure}
	}
	response, err := client.httpClient.Do(request.WithContext(ctx))
	if err != nil {
		return conversationExchange{err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err == nil && int64(len(body)) > client.maxResponseBytes {
		err = errors.New("response too large")
	}
	return conversationExchange{status: response.StatusCode, body: body, header: response.Header, requestID: response.Header.Get("X-Request-Id"), err: err}
}

func validConversationMetadata(metadata map[string]string) bool {
	if len(metadata) > 16 {
		return false
	}
	for key, value := range metadata {
		if key == "" || len(key) > 64 || len(value) > 512 || !utf8.ValidString(key) || !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func validConversationMessages(messages []ConversationMessage) bool {
	if len(messages) > 20 {
		return false
	}
	for _, message := range messages {
		if message.Type != "message" || strings.TrimSpace(message.Content) == "" || len(message.Content) > 65536 {
			return false
		}
		switch message.Role {
		case "user", "assistant", "system", "developer":
		default:
			return false
		}
	}
	return true
}

func decodeConversation(body []byte, expectedID string) (Conversation, bool) {
	var wire struct {
		ID        string            `json:"id"`
		Object    string            `json:"object"`
		CreatedAt int64             `json:"created_at"`
		Metadata  map[string]string `json:"metadata"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Object != "conversation" || !isSafeResponseID(wire.ID) || wire.CreatedAt <= 0 || expectedID != "" && expectedID != wire.ID {
		return Conversation{}, false
	}
	return Conversation{ID: wire.ID, CreatedAt: wire.CreatedAt, Metadata: wire.Metadata}, true
}

// Invoke creates a conversation with a bounded number of possible sends.
func (operation CreateConversationOperation) Invoke(call sdkgo.Call, input CreateConversationRequest) sdkgo.MutationAttempt[Conversation] {
	if !validConversationMetadata(input.Metadata) || !validConversationMessages(input.Items) {
		return sdkgo.NewMutationBranch(CreateConversationBranchDefect, Conversation{}, conversationFailure(sdkgo.FailureValidation, "createConversation", "conversation messages or metadata are invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateConversationDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateConversationBranchDefect, Conversation{}, conversationFailure(sdkgo.FailureLocalDefect, "createConversation", "dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(Conversation{}, *conversationFailure(sdkgo.FailureTransport, "createConversation", "an earlier request may have created a conversation"), sdkgo.Receipt{})
	}
	if decision.Marker.ProviderID != "" {
		read := operation.client.exchangeConversation(call, call.Context, http.MethodGet, "/conversations/"+url.PathEscape(decision.Marker.ProviderID), nil)
		if read.status == 200 {
			conversation, ok := decodeConversation(read.body, decision.Marker.ProviderID)
			if ok {
				return sdkgo.NewMutationBranch(CreateConversationBranchCreated, conversation, nil, responseReceipt(call, read.requestID, read.header, conversation.ID))
			}
		}
		if read.err != nil || read.status == 429 || read.status >= 500 {
			return sdkgo.NewMutationRetry[Conversation](*conversationFailure(sdkgo.FailureAvailability, "createConversation", "conversation read-back is unavailable"), retryAfterHeader(read.header))
		}
		return sdkgo.NewMutationUncertain(Conversation{}, *conversationFailure(sdkgo.FailureTransport, "createConversation", "created conversation could not be confirmed"), responseReceipt(call, read.requestID, read.header, decision.Marker.ProviderID))
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.MaximumPriorSends >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(Conversation{}, *conversationFailure(sdkgo.FailureTransport, "createConversation", "possible sends reached the configured cap without a conversation ID"), sdkgo.Receipt{})
	}
	payload := map[string]any{"items": input.Items, "metadata": input.Metadata}
	var result conversationExchange
	err = fence.Dispatch(func(ctx context.Context) error {
		result = operation.client.exchangeConversation(call, ctx, http.MethodPost, "/conversations", payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[Conversation](*conversationFailure(sdkgo.FailureAvailability, "createConversation", "request was not dispatched"), 0)
	}
	if result.requestFailure != nil {
		return sdkgo.NewMutationBranch(CreateConversationBranchDefect, Conversation{}, result.requestFailure, sdkgo.Receipt{})
	}
	if result.status == 429 || result.status == 503 {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationUncertain(Conversation{}, *conversationFailure(sdkgo.FailureTransport, "createConversation", "retry checkpoint could not be saved"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[Conversation](*conversationFailure(sdkgo.FailureAvailability, "createConversation", "provider requested a retry"), retryAfterHeader(result.header))
	}
	if result.err != nil || result.status >= 500 || result.status == 408 {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(Conversation{}, *conversationFailure(sdkgo.FailureTransport, "createConversation", "possible sends reached the configured cap"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[Conversation](*conversationFailure(sdkgo.FailureAvailability, "createConversation", "provider outcome is unknown"), 0)
	}
	receipt := responseReceipt(call, result.requestID, result.header, "")
	if result.status != 200 {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(Conversation{}, *conversationFailure(sdkgo.FailureTransport, "createConversation", "an earlier send may have created a conversation"), receipt)
		}
		return sdkgo.NewMutationBranch(CreateConversationBranchProviderRejected, Conversation{}, conversationFailure(sdkgo.FailureProviderRejection, "createConversation", "provider rejected conversation creation"), receipt)
	}
	conversation, ok := decodeConversation(result.body, "")
	if !ok {
		return sdkgo.NewMutationUncertain(Conversation{}, *conversationFailure(sdkgo.FailureProtocol, "createConversation", "provider returned invalid conversation data"), receipt)
	}
	// A lost checkpoint can permit another bounded send only if the Step fails to commit this result.
	_ = fence.RecordProviderID(conversation.ID)
	receipt.ProviderObjectID = conversation.ID
	return sdkgo.NewMutationBranch(CreateConversationBranchCreated, conversation, nil, receipt)
}

// Invoke reads one conversation; all transient failures are safe to retry.
func (operation RetrieveConversationOperation) Invoke(call sdkgo.Call, input ConversationIDRequest) sdkgo.QueryAttempt[Conversation] {
	if !isSafeResponseID(input.ConversationID) {
		return sdkgo.NewQueryBranch(RetrieveConversationBranchDefect, Conversation{}, conversationFailure(sdkgo.FailureValidation, "retrieveConversation", "conversation ID is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.exchangeConversation(call, call.Context, http.MethodGet, "/conversations/"+url.PathEscape(input.ConversationID), nil)
	if result.requestFailure != nil {
		return sdkgo.NewQueryBranch(RetrieveConversationBranchDefect, Conversation{}, result.requestFailure, sdkgo.Receipt{})
	}
	if result.err != nil {
		return sdkgo.NewQueryRetry[Conversation](*conversationFailure(sdkgo.FailureTransport, "retrieveConversation", "provider response could not be read"), 0)
	}
	if result.status == 408 || result.status == 429 || result.status >= 500 {
		return sdkgo.NewQueryRetry[Conversation](*conversationFailure(sdkgo.FailureAvailability, "retrieveConversation", "provider is unavailable"), retryAfterHeader(result.header))
	}
	receipt := responseReceipt(call, result.requestID, result.header, input.ConversationID)
	if result.status == 404 {
		return sdkgo.NewQueryBranch(RetrieveConversationBranchNotFound, Conversation{}, conversationFailure(sdkgo.FailureNotFound, "retrieveConversation", "conversation was not found"), receipt)
	}
	if result.status != 200 {
		return sdkgo.NewQueryBranch(RetrieveConversationBranchProviderRejected, Conversation{}, conversationFailure(sdkgo.FailureProviderRejection, "retrieveConversation", "provider rejected conversation lookup"), receipt)
	}
	conversation, ok := decodeConversation(result.body, input.ConversationID)
	if !ok {
		return sdkgo.NewQueryBranch(RetrieveConversationBranchInvalidResponse, Conversation{}, conversationFailure(sdkgo.FailureProtocol, "retrieveConversation", "provider returned invalid conversation"), receipt)
	}
	return sdkgo.NewQueryBranch(RetrieveConversationBranchFound, conversation, nil, receipt)
}

// Invoke replaces conversation metadata; repeating the same replacement is safe.
func (operation UpdateConversationOperation) Invoke(call sdkgo.Call, input UpdateConversationRequest) sdkgo.MutationAttempt[Conversation] {
	if !isSafeResponseID(input.ConversationID) || !validConversationMetadata(input.Metadata) {
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, Conversation{}, conversationFailure(sdkgo.FailureValidation, "updateConversation", "conversation ID or metadata is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.exchangeConversation(call, call.Context, http.MethodPost, "/conversations/"+url.PathEscape(input.ConversationID), map[string]any{"metadata": input.Metadata})
	if result.requestFailure != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, Conversation{}, result.requestFailure, sdkgo.Receipt{})
	}
	if result.err != nil || result.status == 408 || result.status == 429 || result.status >= 500 {
		return sdkgo.NewMutationRetry[Conversation](*conversationFailure(sdkgo.FailureAvailability, "updateConversation", "provider outcome could not be confirmed"), retryAfterHeader(result.header))
	}
	receipt := responseReceipt(call, result.requestID, result.header, input.ConversationID)
	if result.status == 404 {
		return sdkgo.NewMutationBranch(UpdateConversationBranchNotFound, Conversation{}, conversationFailure(sdkgo.FailureNotFound, "updateConversation", "conversation was not found"), receipt)
	}
	if result.status != 200 {
		return sdkgo.NewMutationBranch(UpdateConversationBranchProviderRejected, Conversation{}, conversationFailure(sdkgo.FailureProviderRejection, "updateConversation", "provider rejected metadata update"), receipt)
	}
	conversation, ok := decodeConversation(result.body, input.ConversationID)
	if !ok {
		return sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, Conversation{}, conversationFailure(sdkgo.FailureProtocol, "updateConversation", "provider returned invalid conversation"), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateConversationBranchUpdated, conversation, nil, receipt)
}

// Invoke deletes one conversation; repeated deletion has no additional effect.
func (operation DeleteConversationOperation) Invoke(call sdkgo.Call, input ConversationIDRequest) sdkgo.MutationAttempt[DeletedResponse] {
	if !isSafeResponseID(input.ConversationID) {
		return sdkgo.NewMutationBranch(DeleteConversationBranchDefect, DeletedResponse{}, conversationFailure(sdkgo.FailureValidation, "deleteConversation", "conversation ID is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.exchangeConversation(call, call.Context, http.MethodDelete, "/conversations/"+url.PathEscape(input.ConversationID), nil)
	if result.requestFailure != nil {
		return sdkgo.NewMutationBranch(DeleteConversationBranchDefect, DeletedResponse{}, result.requestFailure, sdkgo.Receipt{})
	}
	if result.err != nil || result.status == 408 || result.status == 429 || result.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedResponse](*conversationFailure(sdkgo.FailureAvailability, "deleteConversation", "provider outcome could not be confirmed"), retryAfterHeader(result.header))
	}
	receipt := responseReceipt(call, result.requestID, result.header, input.ConversationID)
	if result.status == 404 {
		return sdkgo.NewMutationBranch(DeleteConversationBranchNotFound, DeletedResponse{}, conversationFailure(sdkgo.FailureNotFound, "deleteConversation", "conversation was not found"), receipt)
	}
	if result.status != 200 {
		return sdkgo.NewMutationBranch(DeleteConversationBranchProviderRejected, DeletedResponse{}, conversationFailure(sdkgo.FailureProviderRejection, "deleteConversation", "provider rejected conversation deletion"), receipt)
	}
	var deleted struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Deleted bool   `json:"deleted"`
	}
	if json.Unmarshal(result.body, &deleted) != nil || deleted.Object != "conversation.deleted" || deleted.ID != input.ConversationID || !deleted.Deleted {
		return sdkgo.NewMutationBranch(DeleteConversationBranchInvalidResponse, DeletedResponse{}, conversationFailure(sdkgo.FailureProtocol, "deleteConversation", "provider returned invalid deletion receipt"), receipt)
	}
	return sdkgo.NewMutationBranch(DeleteConversationBranchDeleted, DeletedResponse{ID: deleted.ID, Deleted: true}, nil, receipt)
}

func conversationFailure(kind sdkgo.FailureKind, operation, message string) *sdkgo.Failure {
	failure := openAIFailure(kind, operation, message)
	return &failure
}
