// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Message contains one Basecamp message identity and display content.
type Message struct {
	// ID is the numeric message ID.
	ID string `json:"id"`
	// Subject is the message title.
	Subject string `json:"subject"`
	// Content is provider rich text.
	Content string `json:"content,omitempty"`
	// Status is drafted or active.
	Status string `json:"status"`
}

// MessagesPage contains one provider page of messages.
type MessagesPage struct {
	// Messages are active posts on this page.
	Messages []Message `json:"messages"`
}

// ListMessagesInput identifies one message board.
type ListMessagesInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// MessageBoardID comes from getProject's dock.
	MessageBoardID string `json:"messageBoardId"`
}

// GetMessageInput identifies one message.
type GetMessageInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// MessageID comes from listMessages or a Basecamp URL.
	MessageID string `json:"messageId"`
}

// CreateMessageInput supplies a message board and initial content.
type CreateMessageInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// MessageBoardID comes from getProject.
	MessageBoardID string `json:"messageBoardId"`
	// Subject is a non-blank title.
	Subject string `json:"subject"`
	// Content is optional rich text.
	Content string `json:"content,omitempty"`
	// Publish posts immediately when true; false saves a draft without notifying.
	Publish bool `json:"publish,omitempty"`
	// SubscriberIDs are notified on publication; nil uses Basecamp's all-project default.
	SubscriberIDs []string `json:"subscriberIds,omitempty"`
}

// UpdateMessageInput replaces a message's subject and content.
type UpdateMessageInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// MessageID comes from listMessages.
	MessageID string `json:"messageId"`
	// Subject is the non-blank replacement title.
	Subject string `json:"subject"`
	// Content is replacement rich text; blank clears it.
	Content string `json:"content"`
}

// MessageActionResult identifies a message and applied state change.
type MessageActionResult struct {
	// MessageID is the selected message.
	MessageID string `json:"messageId"`
	// Status names the applied action.
	Status string `json:"status"`
}

// ListMessagesOperation reads active messages on one board.
type ListMessagesOperation struct{ client *Client }

// ListMessages binds the message list Query.
func (client *Client) ListMessages() ListMessagesOperation { return ListMessagesOperation{client} }

// Definition returns the generated Query contract.
func (ListMessagesOperation) Definition() sdkgo.QueryDefinition { return ListMessagesDefinition }

// Invoke reads one provider page of active messages.
func (operation ListMessagesOperation) Invoke(call sdkgo.Call, input ListMessagesInput) sdkgo.QueryAttempt[MessagesPage] {
	if !validResourceID(input.AccountID) || !validResourceID(input.MessageBoardID) {
		return resourceBranch[MessagesPage]("listMessages", ListMessagesBranchDefect, sdkgo.FailureValidation, "message board IDs are invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/message_boards/"+input.MessageBoardID+"/messages.json")
	return resourceQueryAttempt(response, "listMessages", ListMessagesBranchListed, "", ListMessagesBranchProviderRejected, ListMessagesBranchInvalidResponse, ListMessagesBranchDefect, decodeMessages)
}

// GetMessageOperation reads one message.
type GetMessageOperation struct{ client *Client }

// GetMessage binds the singular Query.
func (client *Client) GetMessage() GetMessageOperation { return GetMessageOperation{client} }

// Definition returns the generated Query contract.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Invoke reads the selected message.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[Message] {
	if !validResourceID(input.AccountID) || !validResourceID(input.MessageID) {
		return resourceBranch[Message]("getMessage", GetMessageBranchDefect, sdkgo.FailureValidation, "message IDs are invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/messages/"+input.MessageID+".json")
	return resourceQueryAttempt(response, "getMessage", GetMessageBranchFound, GetMessageBranchNotFound, GetMessageBranchProviderRejected, GetMessageBranchInvalidResponse, GetMessageBranchDefect, func(body []byte) (Message, error) { return decodeMessage(body, input.MessageID) })
}

// CreateMessageOperation creates a draft or published message with bounded sends.
type CreateMessageOperation struct{ client *Client }

// CreateMessage binds the message creation Mutation.
func (client *Client) CreateMessage() CreateMessageOperation { return CreateMessageOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateMessageOperation) Definition() sdkgo.MutationDefinition { return CreateMessageDefinition }

// IdempotencyKey records the Dex call identity; Basecamp does not deduplicate it.
func (CreateMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a message, bounding possible duplicate posts and notifications.
func (operation CreateMessageOperation) Invoke(call sdkgo.Call, input CreateMessageInput) sdkgo.MutationAttempt[Message] {
	if !validResourceID(input.AccountID) || !validResourceID(input.MessageBoardID) || strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 512 || len(input.Content) > 100000 {
		return projectMutationBranch[Message]("createMessage", CreateMessageBranchDefect, sdkgo.FailureValidation, "message input is invalid")
	}
	var subscriberIDs []int64
	if input.SubscriberIDs != nil {
		subscriberIDs = make([]int64, 0, len(input.SubscriberIDs))
		for _, value := range input.SubscriberIDs {
			if !validResourceID(value) {
				return projectMutationBranch[Message]("createMessage", CreateMessageBranchDefect, sdkgo.FailureValidation, "subscriber ID is invalid")
			}
			personID, _ := strconv.ParseInt(value, 10, 64)
			subscriberIDs = append(subscriberIDs, personID)
		}
	}
	status := "drafted"
	if input.Publish {
		status = "active"
	}
	requestBody := struct {
		Subject       string   `json:"subject"`
		Content       string   `json:"content,omitempty"`
		Status        string   `json:"status"`
		Subscriptions *[]int64 `json:"subscriptions,omitempty"`
	}{Subject: input.Subject, Content: input.Content, Status: status}
	if input.SubscriberIDs != nil {
		requestBody.Subscriptions = &subscriberIDs
	}
	fence, err := singledispatch.NewFence(call, CreateMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return projectMutationBranch[Message]("createMessage", CreateMessageBranchDefect, sdkgo.FailureLocalDefect, "message dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return projectMutationUncertain[Message]("createMessage", "message creation may have completed without a stable lookup key")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.accountMutation(call, requestContext, http.MethodPost, input.AccountID, "/message_boards/"+input.MessageBoardID+"/messages.json", requestBody)
		return nil
	})
	if err != nil {
		return projectMutationRetry[Message]("createMessage", sdkgo.FailureAvailability, "message dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return projectMutationRetry[Message]("createMessage", sdkgo.FailureAvailability, "message resend checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return projectMutationRetry[Message]("createMessage", kind, "Basecamp temporarily rejected message creation", response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[Message]("createMessage", "an earlier message creation may have completed")
		}
		return projectMutationBranch[Message]("createMessage", CreateMessageBranchDefect, sdkgo.FailureAuthentication, "Basecamp credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return projectMutationRetry[Message]("createMessage", sdkgo.FailureTransport, "message creation outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[Message]("createMessage", "an earlier message creation may have completed")
		}
		return projectMutationBranch[Message]("createMessage", CreateMessageBranchProviderRejected, sdkgo.FailureProviderRejection, "Basecamp rejected message creation")
	}
	message, err := decodeMessage(response.body, "")
	if err != nil || message.Subject != input.Subject || message.Status != status {
		return projectMutationRetry[Message]("createMessage", sdkgo.FailureProtocol, "Basecamp returned invalid created message JSON", 0)
	}
	return sdkgo.NewMutationBranch(CreateMessageBranchCreated, message, nil, sdkgo.Receipt{})
}

// UpdateMessageOperation replaces a message's subject and content.
type UpdateMessageOperation struct{ client *Client }

// UpdateMessage binds the message update Mutation.
func (client *Client) UpdateMessage() UpdateMessageOperation { return UpdateMessageOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateMessageOperation) Definition() sdkgo.MutationDefinition { return UpdateMessageDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the selected message's title and rich text body.
func (operation UpdateMessageOperation) Invoke(call sdkgo.Call, input UpdateMessageInput) sdkgo.MutationAttempt[Message] {
	if !validResourceID(input.AccountID) || !validResourceID(input.MessageID) || strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 512 || len(input.Content) > 100000 {
		return projectMutationBranch[Message]("updateMessage", UpdateMessageBranchDefect, sdkgo.FailureValidation, "message input is invalid")
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/messages/"+input.MessageID+".json", struct {
		Subject string `json:"subject"`
		Content string `json:"content"`
	}{input.Subject, input.Content})
	if attempt, done := projectMutationResponse[Message](response, "updateMessage", http.StatusOK, UpdateMessageBranchNotFound, UpdateMessageBranchProviderRejected, UpdateMessageBranchDefect, UpdateMessageBranchInvalidResponse); done {
		return attempt
	}
	message, err := decodeMessage(response.body, input.MessageID)
	if err != nil || message.Subject != input.Subject {
		return projectMutationBranch[Message]("updateMessage", UpdateMessageBranchInvalidResponse, sdkgo.FailureProtocol, "Basecamp returned invalid updated message JSON")
	}
	return sdkgo.NewMutationBranch(UpdateMessageBranchUpdated, message, nil, sdkgo.Receipt{})
}

// PublishMessageOperation publishes a draft exactly once according to Basecamp.
type PublishMessageOperation struct{ client *Client }

// PublishMessage binds the draft publication Mutation.
func (client *Client) PublishMessage() PublishMessageOperation {
	return PublishMessageOperation{client}
}

// Definition returns the generated Mutation contract.
func (PublishMessageOperation) Definition() sdkgo.MutationDefinition { return PublishMessageDefinition }

// IdempotencyKey records the stable Dex call identity.
func (PublishMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ GetMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke publishes the selected draft while preserving its other fields.
func (operation PublishMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.MutationAttempt[Message] {
	if !validResourceID(input.AccountID) || !validResourceID(input.MessageID) {
		return projectMutationBranch[Message]("publishMessage", PublishMessageBranchDefect, sdkgo.FailureValidation, "message IDs are invalid")
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/messages/"+input.MessageID+".json", struct {
		Status string `json:"status"`
	}{"active"})
	if attempt, done := projectMutationResponse[Message](response, "publishMessage", http.StatusOK, PublishMessageBranchNotFound, PublishMessageBranchProviderRejected, PublishMessageBranchDefect, PublishMessageBranchInvalidResponse); done {
		return attempt
	}
	message, err := decodeMessage(response.body, input.MessageID)
	if err != nil || message.Status != "active" {
		return projectMutationBranch[Message]("publishMessage", PublishMessageBranchInvalidResponse, sdkgo.FailureProtocol, "Basecamp returned invalid published message JSON")
	}
	return sdkgo.NewMutationBranch(PublishMessageBranchPublished, message, nil, sdkgo.Receipt{})
}

// PinMessageOperation pins a message.
type PinMessageOperation struct{ client *Client }

// PinMessage binds the pin Mutation.
func (client *Client) PinMessage() PinMessageOperation { return PinMessageOperation{client} }

// Definition returns the generated Mutation contract.
func (PinMessageOperation) Definition() sdkgo.MutationDefinition { return PinMessageDefinition }

// IdempotencyKey records the stable Dex call identity.
func (PinMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ GetMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke pins the selected message.
func (operation PinMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.MutationAttempt[MessageActionResult] {
	return operation.client.changeMessageState(call, input, http.MethodPost, "/recordings/", "/pin.json", "pinMessage", "pinned", PinMessageBranchPinned, PinMessageBranchNotFound, PinMessageBranchProviderRejected, PinMessageBranchDefect)
}

// UnpinMessageOperation removes a message pin.
type UnpinMessageOperation struct{ client *Client }

// UnpinMessage binds the unpin Mutation.
func (client *Client) UnpinMessage() UnpinMessageOperation { return UnpinMessageOperation{client} }

// Definition returns the generated Mutation contract.
func (UnpinMessageOperation) Definition() sdkgo.MutationDefinition { return UnpinMessageDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UnpinMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ GetMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes the selected message's pin.
func (operation UnpinMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.MutationAttempt[MessageActionResult] {
	return operation.client.changeMessageState(call, input, http.MethodDelete, "/recordings/", "/pin.json", "unpinMessage", "unpinned", UnpinMessageBranchUnpinned, UnpinMessageBranchNotFound, UnpinMessageBranchProviderRejected, UnpinMessageBranchDefect)
}

// TrashMessageOperation trashes a message recording.
type TrashMessageOperation struct{ client *Client }

// TrashMessage binds the trash Mutation.
func (client *Client) TrashMessage() TrashMessageOperation { return TrashMessageOperation{client} }

// Definition returns the generated Mutation contract.
func (TrashMessageOperation) Definition() sdkgo.MutationDefinition { return TrashMessageDefinition }

// IdempotencyKey records the stable Dex call identity.
func (TrashMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ GetMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke trashes the selected message recording.
func (operation TrashMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.MutationAttempt[MessageActionResult] {
	return operation.client.changeMessageState(call, input, http.MethodPut, "/recordings/", "/status/trashed.json", "trashMessage", "trashed", TrashMessageBranchTrashed, TrashMessageBranchNotFound, TrashMessageBranchProviderRejected, TrashMessageBranchDefect)
}

func (client *Client) changeMessageState(call sdkgo.Call, input GetMessageInput, method, prefix, suffix, operation, status string, success, notFound, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[MessageActionResult] {
	if !validResourceID(input.AccountID) || !validResourceID(input.MessageID) {
		return projectMutationBranch[MessageActionResult](operation, defect, sdkgo.FailureValidation, "message IDs are invalid")
	}
	response := client.accountMutation(call, call.Context, method, input.AccountID, prefix+input.MessageID+suffix, nil)
	if attempt, done := projectMutationResponse[MessageActionResult](response, operation, http.StatusNoContent, notFound, rejected, defect, rejected); done {
		return attempt
	}
	return sdkgo.NewMutationBranch(success, MessageActionResult{MessageID: input.MessageID, Status: status}, nil, sdkgo.Receipt{})
}

func decodeMessages(body []byte) (MessagesPage, error) {
	var records []struct {
		ID      int64  `json:"id"`
		Title   string `json:"title"`
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil {
		return MessagesPage{}, fmt.Errorf("messages are invalid")
	}
	page := MessagesPage{Messages: make([]Message, 0, len(records))}
	for _, record := range records {
		if record.ID <= 0 || strings.TrimSpace(record.Title) == "" || record.Status == "" {
			return MessagesPage{}, fmt.Errorf("message is invalid")
		}
		page.Messages = append(page.Messages, Message{ID: strconv.FormatInt(record.ID, 10), Subject: record.Title, Content: record.Content, Status: record.Status})
	}
	return page, nil
}

func decodeMessage(body []byte, expectedID string) (Message, error) {
	var record struct {
		ID      int64  `json:"id"`
		Title   string `json:"title"`
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(body, &record); err != nil || record.ID <= 0 || (expectedID != "" && strconv.FormatInt(record.ID, 10) != expectedID) || strings.TrimSpace(record.Title) == "" || record.Status == "" {
		return Message{}, fmt.Errorf("message is invalid")
	}
	return Message{ID: strconv.FormatInt(record.ID, 10), Subject: record.Title, Content: record.Content, Status: record.Status}, nil
}
