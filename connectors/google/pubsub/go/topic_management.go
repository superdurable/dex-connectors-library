// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pubsub

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var topicNamePattern = regexp.MustCompile(`^projects/[A-Za-z0-9][A-Za-z0-9:._~-]{0,255}/topics/[A-Za-z][A-Za-z0-9._~+%-]{2,254}$`)

// GetTopicInput identifies one Pub/Sub topic.
type GetTopicInput struct {
	// Name is a full projects/{project}/topics/{topic} resource name.
	Name string `json:"name"`
}

// CreateTopicInput creates a topic at a stable name.
type CreateTopicInput struct {
	// Name is the full resource name chosen by the caller.
	Name string `json:"name"`
	// Labels are optional provider labels assigned at creation.
	Labels map[string]string `json:"labels,omitempty"`
}

// UpdateTopicLabelsInput replaces all labels of an existing topic.
type UpdateTopicLabelsInput struct {
	// Name is the full resource name of the topic.
	Name string `json:"name"`
	// Labels is the complete replacement map; an empty map clears all labels.
	Labels map[string]string `json:"labels"`
}

// DeleteTopicInput identifies a topic to delete.
type DeleteTopicInput struct {
	// Name is the full resource name of the topic.
	Name string `json:"name"`
}

// DeletedTopic confirms the topic name removed by the request.
type DeletedTopic struct {
	// Name is the requested topic name.
	Name string `json:"name"`
}

// GetTopicOperation reads one topic.
type GetTopicOperation struct{ client *Client }

// Definition returns the generated topic read contract.
func (GetTopicOperation) Definition() sdkgo.QueryDefinition { return GetTopicDefinition }

// GetTopic binds the topic Query to the client.
func (client *Client) GetTopic() GetTopicOperation { return GetTopicOperation{client} }

// Invoke reads the named topic.
func (operation GetTopicOperation) Invoke(call sdkgo.Call, input GetTopicInput) sdkgo.QueryAttempt[Topic] {
	if !validTopicName(input.Name) {
		return topicManagementQueryBranch(GetTopicBranchDefect, Topic{}, sdkgo.FailureValidation, "Pub/Sub topic name is invalid")
	}
	response := operation.client.get(call, "/v1/"+input.Name)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementQueryFailure[Topic](*failure, GetTopicBranchDefect, GetTopicBranchInvalidResponse)
	}
	if response.status == http.StatusNotFound {
		return topicManagementQueryBranch(GetTopicBranchNotFound, Topic{}, sdkgo.FailureNotFound, "Pub/Sub topic was not found")
	}
	if response.status != http.StatusOK {
		return topicManagementQueryStatus[Topic](response, GetTopicBranchProviderRejected, "getTopic")
	}
	topic, isValid := decodeTopic(response.body, input.Name)
	if !isValid {
		return topicManagementQueryBranch(GetTopicBranchInvalidResponse, Topic{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid topic JSON")
	}
	return sdkgo.NewQueryBranch(GetTopicBranchFound, topic, nil, sdkgo.Receipt{})
}

// CreateTopicOperation creates a topic at a stable name.
type CreateTopicOperation struct{ client *Client }

// Definition returns the generated topic create contract.
func (CreateTopicOperation) Definition() sdkgo.MutationDefinition { return CreateTopicDefinition }

// IdempotencyKey derives a stable call key for the logical create.
func (CreateTopicOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTopicInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// CreateTopic binds the topic Mutation to the client.
func (client *Client) CreateTopic() CreateTopicOperation { return CreateTopicOperation{client} }

// Invoke creates the named topic and confirms a matching topic on conflict.
func (operation CreateTopicOperation) Invoke(call sdkgo.Call, input CreateTopicInput) sdkgo.MutationAttempt[Topic] {
	if !validTopicName(input.Name) || !validTopicLabels(input.Labels) {
		return topicManagementMutationBranch(CreateTopicBranchDefect, Topic{}, sdkgo.FailureValidation, "Pub/Sub topic name or labels are invalid")
	}
	body, _ := json.Marshal(struct {
		Labels map[string]string `json:"labels,omitempty"`
	}{input.Labels})
	response := operation.client.request(call, http.MethodPut, "/v1/"+input.Name, body)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementMutationFailure[Topic](*failure, CreateTopicBranchDefect, CreateTopicBranchInvalidResponse)
	}
	if response.status == http.StatusConflict {
		read := operation.client.get(call, "/v1/"+input.Name)
		if read.err != nil || read.status >= 500 {
			return sdkgo.NewMutationRetry[Topic](topicManagementFailure("createTopic", sdkgo.FailureAvailability, "Pub/Sub topic read-back failed"), 0)
		}
		if read.status == http.StatusOK {
			topic, isValid := decodeTopic(read.body, input.Name)
			if isValid && sameTopicLabels(topic.Labels, input.Labels) {
				return sdkgo.NewMutationBranch(CreateTopicBranchCreated, topic, nil, topicManagementReceipt(call, topic.Name))
			}
		}
		return topicManagementMutationBranch(CreateTopicBranchProviderRejected, Topic{}, sdkgo.FailureConflict, "Pub/Sub topic already exists with different labels")
	}
	if response.status != http.StatusOK {
		return topicManagementMutationStatus[Topic](response, CreateTopicBranchProviderRejected, "createTopic")
	}
	topic, isValid := decodeTopic(response.body, input.Name)
	if !isValid {
		return topicManagementMutationBranch(CreateTopicBranchInvalidResponse, Topic{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid created topic JSON")
	}
	return sdkgo.NewMutationBranch(CreateTopicBranchCreated, topic, nil, topicManagementReceipt(call, topic.Name))
}

// UpdateTopicLabelsOperation replaces one topic's labels.
type UpdateTopicLabelsOperation struct{ client *Client }

// Definition returns the generated topic update contract.
func (UpdateTopicLabelsOperation) Definition() sdkgo.MutationDefinition {
	return UpdateTopicLabelsDefinition
}

// IdempotencyKey derives a stable call key for the logical update.
func (UpdateTopicLabelsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTopicLabelsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// UpdateTopicLabels binds the topic update Mutation to the client.
func (client *Client) UpdateTopicLabels() UpdateTopicLabelsOperation {
	return UpdateTopicLabelsOperation{client}
}

// Invoke replaces all topic labels with an absolute update mask.
func (operation UpdateTopicLabelsOperation) Invoke(call sdkgo.Call, input UpdateTopicLabelsInput) sdkgo.MutationAttempt[Topic] {
	if !validTopicName(input.Name) || input.Labels == nil || !validTopicLabels(input.Labels) {
		return topicManagementMutationBranch(UpdateTopicLabelsBranchDefect, Topic{}, sdkgo.FailureValidation, "Pub/Sub topic name or labels are invalid")
	}
	body, _ := json.Marshal(struct {
		Topic      Topic  `json:"topic"`
		UpdateMask string `json:"updateMask"`
	}{Topic: Topic{Name: input.Name, Labels: input.Labels}, UpdateMask: "labels"})
	response := operation.client.request(call, http.MethodPatch, "/v1/"+input.Name, body)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementMutationFailure[Topic](*failure, UpdateTopicLabelsBranchDefect, UpdateTopicLabelsBranchInvalidResponse)
	}
	if response.status == http.StatusNotFound {
		return topicManagementMutationBranch(UpdateTopicLabelsBranchNotFound, Topic{}, sdkgo.FailureNotFound, "Pub/Sub topic was not found")
	}
	if response.status != http.StatusOK {
		return topicManagementMutationStatus[Topic](response, UpdateTopicLabelsBranchProviderRejected, "updateTopicLabels")
	}
	topic, isValid := decodeTopic(response.body, input.Name)
	if !isValid || !sameTopicLabels(topic.Labels, input.Labels) {
		return topicManagementMutationBranch(UpdateTopicLabelsBranchInvalidResponse, Topic{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid updated topic JSON")
	}
	return sdkgo.NewMutationBranch(UpdateTopicLabelsBranchUpdated, topic, nil, topicManagementReceipt(call, topic.Name))
}

// DeleteTopicOperation deletes a named topic.
type DeleteTopicOperation struct{ client *Client }

// Definition returns the generated topic delete contract.
func (DeleteTopicOperation) Definition() sdkgo.MutationDefinition { return DeleteTopicDefinition }

// IdempotencyKey derives a stable call key for the logical deletion.
func (DeleteTopicOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTopicInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteTopic binds the delete Mutation to the client.
func (client *Client) DeleteTopic() DeleteTopicOperation { return DeleteTopicOperation{client} }

// Invoke deletes the named topic; a missing topic is already deleted.
func (operation DeleteTopicOperation) Invoke(call sdkgo.Call, input DeleteTopicInput) sdkgo.MutationAttempt[DeletedTopic] {
	if !validTopicName(input.Name) {
		return topicManagementMutationBranch(DeleteTopicBranchDefect, DeletedTopic{}, sdkgo.FailureValidation, "Pub/Sub topic name is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/v1/"+input.Name, nil)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementMutationFailure[DeletedTopic](*failure, DeleteTopicBranchDefect, DeleteTopicBranchProviderRejected)
	}
	if response.status == http.StatusOK || response.status == http.StatusNoContent || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteTopicBranchDeleted, DeletedTopic{Name: input.Name}, nil, topicManagementReceipt(call, input.Name))
	}
	return topicManagementMutationStatus[DeletedTopic](response, DeleteTopicBranchProviderRejected, "deleteTopic")
}

func validTopicName(name string) bool { return topicNamePattern.MatchString(name) }
func validTopicLabels(labels map[string]string) bool {
	if len(labels) > 64 {
		return false
	}
	for key, value := range labels {
		if key == "" || len(key) > 63 || len(value) > 63 || strings.ContainsAny(key+value, "\r\n") {
			return false
		}
	}
	return true
}
func decodeTopic(body []byte, name string) (Topic, bool) {
	var topic Topic
	if json.Unmarshal(body, &topic) != nil || topic.Name != name || !validTopicLabels(topic.Labels) {
		return Topic{}, false
	}
	return topic, true
}
func sameTopicLabels(first, second map[string]string) bool {
	if len(first) != len(second) {
		return false
	}
	for key, value := range first {
		if second[key] != value {
			return false
		}
	}
	return true
}
func topicManagementResponseFailure(response providerResponse) *sdkgo.Failure {
	if response.err == nil {
		return nil
	}
	if errors.Is(response.err, errCredential) {
		failure := topicManagementFailure("topicManagement", sdkgo.FailureAuthentication, "Pub/Sub credentials are unavailable")
		return &failure
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		failure := topicManagementFailure("topicManagement", sdkgo.FailureResponseTooLarge, "Pub/Sub response exceeded the configured limit")
		return &failure
	}
	failure := topicManagementFailure("topicManagement", sdkgo.FailureTransport, "Pub/Sub request failed before a response was received")
	return &failure
}
func topicManagementQueryFailure[T any](failure sdkgo.Failure, defectBranch, invalidBranch sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	if failure.Kind == sdkgo.FailureAuthentication {
		return sdkgo.NewQueryBranch(defectBranch, *new(T), &failure, sdkgo.Receipt{})
	}
	if failure.Kind == sdkgo.FailureResponseTooLarge {
		return sdkgo.NewQueryBranch(invalidBranch, *new(T), &failure, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryRetry[T](failure, 0)
}
func topicManagementMutationFailure[T any](failure sdkgo.Failure, defectBranch, invalidBranch sdkgo.BranchID) sdkgo.MutationAttempt[T] {
	if failure.Kind == sdkgo.FailureAuthentication {
		return sdkgo.NewMutationBranch(defectBranch, *new(T), &failure, sdkgo.Receipt{})
	}
	if failure.Kind == sdkgo.FailureResponseTooLarge {
		return sdkgo.NewMutationBranch(invalidBranch, *new(T), &failure, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationRetry[T](failure, 0)
}
func topicManagementQueryStatus[T any](response providerResponse, rejectedBranch sdkgo.BranchID, operationID string) sdkgo.QueryAttempt[T] {
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](topicManagementFailure(operationID, sdkgo.FailureAvailability, "Pub/Sub is temporarily unavailable"), response.retryAfter)
	}
	return topicManagementQueryBranch(rejectedBranch, *new(T), sdkgo.FailureProviderRejection, "Pub/Sub rejected HTTP "+strconv.Itoa(response.status))
}
func topicManagementMutationStatus[T any](response providerResponse, rejectedBranch sdkgo.BranchID, operationID string) sdkgo.MutationAttempt[T] {
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[T](topicManagementFailure(operationID, sdkgo.FailureAvailability, "Pub/Sub is temporarily unavailable"), response.retryAfter)
	}
	return topicManagementMutationBranch(rejectedBranch, *new(T), sdkgo.FailureProviderRejection, "Pub/Sub rejected HTTP "+strconv.Itoa(response.status))
}
func topicManagementQueryBranch[T any](branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := topicManagementFailure("topicManagement", kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func topicManagementMutationBranch[T any](branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := topicManagementFailure("topicManagement", kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
func topicManagementFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "google-pubsub", Operation: operationID, Message: message}
}
func topicManagementReceipt(call sdkgo.Call, name string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, Provider: "google-pubsub", ProviderObjectID: name, ObservedAt: time.Now().UTC()}
}
