// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateTopicInput creates a topic with an immutable default subscription.
type CreateTopicInput struct {
	// Name is the topic name, up to 50 characters.
	Name string `json:"name"`
	// DefaultSubscription is opt_in or opt_out.
	DefaultSubscription string `json:"defaultSubscription"`
	// Description is optional text, up to 200 characters.
	Description string `json:"description,omitempty"`
	// Visibility is public or private; blank uses Resend's private default.
	Visibility string `json:"visibility,omitempty"`
}

// GetTopicInput selects a topic by provider ID.
type GetTopicInput struct {
	// ID is the topic ID from Resend's Topics page.
	ID string `json:"id"`
}

// ListTopicsInput selects a bounded topic page.
type ListTopicsInput struct {
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is an exclusive topic ID cursor.
	After string `json:"after,omitempty"`
}

// UpdateTopicInput changes selected absolute topic fields.
type UpdateTopicInput struct {
	// ID is the topic ID.
	ID string `json:"id"`
	// Name changes the topic name when non-nil.
	Name *string `json:"name,omitempty"`
	// Description changes the description when non-nil; blank clears it.
	Description *string `json:"description,omitempty"`
	// Visibility changes public or private visibility when non-nil.
	Visibility *string `json:"visibility,omitempty"`
}

// DeleteTopicInput selects a topic for deletion.
type DeleteTopicInput struct {
	// ID is the topic ID.
	ID string `json:"id"`
}

// TopicReference identifies a created or updated topic.
type TopicReference struct {
	// ID is the stable provider topic ID.
	ID string `json:"id"`
}

// Topic contains selected topic fields.
type Topic struct {
	// ID is the stable provider topic ID.
	ID string `json:"id"`
	// Name is the topic name.
	Name string `json:"name"`
	// Description is optional text.
	Description string `json:"description"`
	// DefaultSubscription is opt_in or opt_out.
	DefaultSubscription string `json:"default_subscription"`
	// Visibility is public or private.
	Visibility string `json:"visibility"`
}

// TopicPage contains one page of topics.
type TopicPage struct {
	// Data contains topic summaries.
	Data []Topic `json:"data"`
	// HasMore indicates that another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedTopic records a provider deletion acknowledgement.
type DeletedTopic struct {
	// ID is the deleted topic ID.
	ID string `json:"id"`
	// Deleted confirms that deletion succeeded.
	Deleted bool `json:"deleted"`
}

// CreateTopicOperation creates a topic with bounded resend.
type CreateTopicOperation struct{ client *Client }

// GetTopicOperation retrieves a topic.
type GetTopicOperation struct{ client *Client }

// ListTopicsOperation lists topics.
type ListTopicsOperation struct{ client *Client }

// UpdateTopicOperation changes a topic.
type UpdateTopicOperation struct{ client *Client }

// DeleteTopicOperation removes a topic.
type DeleteTopicOperation struct{ client *Client }

// CreateTopic binds topic creation to this client.
func (client *Client) CreateTopic() CreateTopicOperation { return CreateTopicOperation{client} }

// GetTopic binds topic retrieval to this client.
func (client *Client) GetTopic() GetTopicOperation { return GetTopicOperation{client} }

// ListTopics binds topic listing to this client.
func (client *Client) ListTopics() ListTopicsOperation { return ListTopicsOperation{client} }

// UpdateTopic binds topic update to this client.
func (client *Client) UpdateTopic() UpdateTopicOperation { return UpdateTopicOperation{client} }

// DeleteTopic binds topic deletion to this client.
func (client *Client) DeleteTopic() DeleteTopicOperation { return DeleteTopicOperation{client} }

// Definition returns the generated createTopic contract.
func (CreateTopicOperation) Definition() sdkgo.MutationDefinition { return CreateTopicDefinition }

// Definition returns the generated getTopic contract.
func (GetTopicOperation) Definition() sdkgo.QueryDefinition { return GetTopicDefinition }

// Definition returns the generated listTopics contract.
func (ListTopicsOperation) Definition() sdkgo.QueryDefinition { return ListTopicsDefinition }

// Definition returns the generated updateTopic contract.
func (UpdateTopicOperation) Definition() sdkgo.MutationDefinition { return UpdateTopicDefinition }

// Definition returns the generated deleteTopic contract.
func (DeleteTopicOperation) Definition() sdkgo.MutationDefinition { return DeleteTopicDefinition }

// IdempotencyKey correlates topic creation attempts without asserting provider deduplication.
func (CreateTopicOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTopicInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute topic updates.
func (UpdateTopicOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTopicInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeat deletions.
func (DeleteTopicOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTopicInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a topic and confirms a possible earlier send by listing topics.
func (operation CreateTopicOperation) Invoke(call sdkgo.Call, input CreateTopicInput) sdkgo.MutationAttempt[TopicReference] {
	if !validTopicName(input.Name) || (input.DefaultSubscription != "opt_in" && input.DefaultSubscription != "opt_out") || len(input.Description) > 200 || !validTopicVisibility(input.Visibility) {
		return mutationFailure[TopicReference]("createTopic", CreateTopicBranchDefect, sdkgo.FailureValidation, "topic name, subscription, description, or visibility is invalid")
	}
	fields := map[string]any{"name": input.Name, "default_subscription": input.DefaultSubscription}
	if input.Description != "" {
		fields["description"] = input.Description
	}
	if input.Visibility != "" {
		fields["visibility"] = input.Visibility
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[TopicReference]("createTopic", CreateTopicBranchDefect, sdkgo.FailureValidation, "topic cannot be encoded")
	}
	request := resendLimitedCreate[TopicReference]{client: operation.client, call: call, operation: "createTopic", path: "/topics", body: body, timeout: CreateTopicDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateTopicBranchCreated, rejected: CreateTopicBranchProviderRejected, defect: CreateTopicBranchDefect, decode: decodeTopicReference, readBack: func(call sdkgo.Call) (TopicReference, readBackState) { return operation.readBackTopic(call, input) }}
	return request.invoke()
}

// Invoke retrieves a topic by ID.
func (operation GetTopicOperation) Invoke(call sdkgo.Call, input GetTopicInput) sdkgo.QueryAttempt[Topic] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[Topic]("getTopic", GetTopicBranchDefect, sdkgo.FailureValidation, "topic ID is required")
	}
	return runQuery(operation.client, call, "getTopic", "/topics/"+url.PathEscape(input.ID), GetTopicBranchFound, GetTopicBranchNotFound, GetTopicBranchProviderRejected, GetTopicBranchInvalidResponse, GetTopicBranchDefect, decodeTopic)
}

// Invoke lists a bounded topic page.
func (operation ListTopicsOperation) Invoke(call sdkgo.Call, input ListTopicsInput) sdkgo.QueryAttempt[TopicPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[TopicPage]("listTopics", ListTopicsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listTopics", "/topics"+query, ListTopicsBranchListed, "", ListTopicsBranchProviderRejected, ListTopicsBranchInvalidResponse, ListTopicsBranchDefect, decodeTopicPage)
}

// Invoke sets selected absolute topic fields.
func (operation UpdateTopicOperation) Invoke(call sdkgo.Call, input UpdateTopicInput) sdkgo.MutationAttempt[TopicReference] {
	if strings.TrimSpace(input.ID) == "" || (input.Name == nil && input.Description == nil && input.Visibility == nil) || (input.Name != nil && !validTopicName(*input.Name)) || (input.Description != nil && len(*input.Description) > 200) || (input.Visibility != nil && !validTopicVisibility(*input.Visibility)) {
		return mutationFailure[TopicReference]("updateTopic", UpdateTopicBranchDefect, sdkgo.FailureValidation, "topic ID and valid selected fields are required")
	}
	fields := map[string]any{}
	if input.Name != nil {
		fields["name"] = *input.Name
	}
	if input.Description != nil {
		fields["description"] = *input.Description
	}
	if input.Visibility != nil {
		fields["visibility"] = *input.Visibility
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[TopicReference]("updateTopic", UpdateTopicBranchDefect, sdkgo.FailureValidation, "topic cannot be encoded")
	}
	return runMutation(operation.client, call, "updateTopic", http.MethodPatch, "/topics/"+url.PathEscape(input.ID), body, "", "", UpdateTopicBranchUpdated, UpdateTopicBranchNotFound, UpdateTopicBranchProviderRejected, UpdateTopicBranchInvalidResponse, UpdateTopicBranchDefect, decodeTopicReference)
}

// Invoke deletes a topic by ID.
func (operation DeleteTopicOperation) Invoke(call sdkgo.Call, input DeleteTopicInput) sdkgo.MutationAttempt[DeletedTopic] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedTopic]("deleteTopic", DeleteTopicBranchDefect, sdkgo.FailureValidation, "topic ID is required")
	}
	return runMutation(operation.client, call, "deleteTopic", http.MethodDelete, "/topics/"+url.PathEscape(input.ID), nil, "", "", DeleteTopicBranchDeleted, DeleteTopicBranchNotFound, DeleteTopicBranchProviderRejected, DeleteTopicBranchInvalidResponse, DeleteTopicBranchDefect, decodeDeletedTopic)
}

func (operation CreateTopicOperation) readBackTopic(call sdkgo.Call, input CreateTopicInput) (TopicReference, readBackState) {
	cursor := ""
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query, _ := listQuery(100, cursor, false)
		body, state := operation.client.readBackResource(call, "/topics"+query)
		if state != readBackMatched {
			return TopicReference{}, readBackRetry
		}
		page, err := decodeTopicPage(body)
		if err != nil {
			return TopicReference{}, readBackRetry
		}
		for _, topic := range page.Data {
			if strings.EqualFold(topic.Name, input.Name) {
				if topic.DefaultSubscription != input.DefaultSubscription || topic.Description != input.Description || topic.Visibility != topicVisibility(input.Visibility) {
					return TopicReference{}, readBackConflicted
				}
				return TopicReference{ID: topic.ID}, readBackMatched
			}
		}
		if !page.HasMore {
			return TopicReference{}, readBackAbsent
		}
		if len(page.Data) == 0 || page.Data[len(page.Data)-1].ID == cursor {
			return TopicReference{}, readBackRetry
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return TopicReference{}, readBackRetry
}

func decodeTopicReference(body []byte) (TopicReference, error) {
	var result TopicReference
	err := json.Unmarshal(body, &result)
	if err == nil && result.ID == "" {
		err = fmt.Errorf("topic ID missing")
	}
	return result, err
}
func decodeTopic(body []byte) (Topic, error) {
	var result Topic
	err := json.Unmarshal(body, &result)
	if err == nil && (result.ID == "" || result.Name == "") {
		err = fmt.Errorf("topic fields missing")
	}
	return result, err
}
func decodeTopicPage(body []byte) (TopicPage, error) {
	var result TopicPage
	err := json.Unmarshal(body, &result)
	if err == nil && result.Data == nil {
		err = fmt.Errorf("topic page data missing")
	}
	return result, err
}
func decodeDeletedTopic(body []byte) (DeletedTopic, error) {
	var result DeletedTopic
	err := json.Unmarshal(body, &result)
	if err == nil && (!result.Deleted || result.ID == "") {
		err = fmt.Errorf("topic deletion acknowledgement missing")
	}
	return result, err
}
func validTopicName(name string) bool { return strings.TrimSpace(name) != "" && len(name) <= 50 }
func validTopicVisibility(visibility string) bool {
	return visibility == "" || visibility == "public" || visibility == "private"
}
func topicVisibility(visibility string) string {
	if visibility == "" {
		return "private"
	}
	return visibility
}
