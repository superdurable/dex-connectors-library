// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pubsub

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var subscriptionNamePattern = regexp.MustCompile(`^projects/[A-Za-z0-9][A-Za-z0-9:._~-]{0,255}/subscriptions/[A-Za-z][A-Za-z0-9._~+%-]{2,254}$`)

// Subscription is the supported pull subscription configuration.
type Subscription struct {
	// Name is the full subscription resource name.
	Name string `json:"name"`
	// Topic is the full source topic resource name.
	Topic string `json:"topic"`
	// AckDeadlineSeconds is the provider's acknowledgement deadline in seconds.
	AckDeadlineSeconds int `json:"ackDeadlineSeconds,omitempty"`
	// Labels are provider metadata labels.
	Labels map[string]string `json:"labels,omitempty"`
}

// SubscriptionsPage contains one bounded list page.
type SubscriptionsPage struct {
	// Subscriptions are the returned subscriptions.
	Subscriptions []Subscription `json:"subscriptions"`
	// NextPageToken is passed to a later list request.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListSubscriptionsInput selects one project page.
type ListSubscriptionsInput struct {
	// ProjectID is the Google Cloud project ID.
	ProjectID string `json:"projectId"`
	// PageSize is 1 through 100; zero uses the provider default.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is a token from a previous page.
	PageToken string `json:"pageToken,omitempty"`
}

// GetSubscriptionInput identifies a subscription.
type GetSubscriptionInput struct {
	// Name is the full subscription resource name.
	Name string `json:"name"`
}

// CreateSubscriptionInput creates a pull subscription at a stable name.
type CreateSubscriptionInput struct {
	// Name is the full subscription resource name.
	Name string `json:"name"`
	// Topic is the full topic resource name.
	Topic string `json:"topic"`
	// AckDeadlineSeconds is 10 through 600, or zero for the provider default.
	AckDeadlineSeconds int `json:"ackDeadlineSeconds,omitempty"`
	// Labels are optional provider labels.
	Labels map[string]string `json:"labels,omitempty"`
}

// UpdateSubscriptionLabelsInput replaces all labels of one subscription.
type UpdateSubscriptionLabelsInput struct {
	// Name is the full subscription resource name.
	Name string `json:"name"`
	// Labels is the complete replacement map; empty clears the labels.
	Labels map[string]string `json:"labels"`
}

// DeleteSubscriptionInput identifies one subscription to delete.
type DeleteSubscriptionInput struct {
	// Name is the full subscription resource name.
	Name string `json:"name"`
}

// DeletedSubscription confirms the requested resource name.
type DeletedSubscription struct {
	// Name is the requested subscription resource name.
	Name string `json:"name"`
}

// ListSubscriptionsOperation reads one page of project subscriptions.
type ListSubscriptionsOperation struct{ client *Client }

// Definition returns the generated list contract.
func (ListSubscriptionsOperation) Definition() sdkgo.QueryDefinition {
	return ListSubscriptionsDefinition
}

// ListSubscriptions binds the list Query to the client.
func (client *Client) ListSubscriptions() ListSubscriptionsOperation {
	return ListSubscriptionsOperation{client}
}

// Invoke reads one page of subscriptions.
func (operation ListSubscriptionsOperation) Invoke(call sdkgo.Call, input ListSubscriptionsInput) sdkgo.QueryAttempt[SubscriptionsPage] {
	if !validProjectID(input.ProjectID) || input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return topicManagementQueryBranch(ListSubscriptionsBranchDefect, SubscriptionsPage{}, sdkgo.FailureValidation, "Pub/Sub project or pagination is invalid")
	}
	parameters := url.Values{}
	if input.PageSize > 0 {
		parameters.Set("pageSize", strconv.Itoa(input.PageSize))
	}
	if input.PageToken != "" {
		parameters.Set("pageToken", input.PageToken)
	}
	path := "/v1/projects/" + input.ProjectID + "/subscriptions"
	if len(parameters) > 0 {
		path += "?" + parameters.Encode()
	}
	response := operation.client.get(call, path)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementQueryFailure[SubscriptionsPage](*failure, ListSubscriptionsBranchDefect, ListSubscriptionsBranchInvalidResponse)
	}
	if response.status != http.StatusOK {
		return topicManagementQueryStatus[SubscriptionsPage](response, ListSubscriptionsBranchProviderRejected, "listSubscriptions")
	}
	var page SubscriptionsPage
	if json.Unmarshal(response.body, &page) != nil {
		return topicManagementQueryBranch(ListSubscriptionsBranchInvalidResponse, SubscriptionsPage{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid subscription page JSON")
	}
	for _, subscription := range page.Subscriptions {
		if !validSubscription(subscription, subscription.Name) {
			return topicManagementQueryBranch(ListSubscriptionsBranchInvalidResponse, SubscriptionsPage{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid subscription")
		}
	}
	return sdkgo.NewQueryBranch(ListSubscriptionsBranchListed, page, nil, sdkgo.Receipt{})
}

// GetSubscriptionOperation reads one subscription.
type GetSubscriptionOperation struct{ client *Client }

// Definition returns the generated get contract.
func (GetSubscriptionOperation) Definition() sdkgo.QueryDefinition { return GetSubscriptionDefinition }

// GetSubscription binds the get Query to the client.
func (client *Client) GetSubscription() GetSubscriptionOperation {
	return GetSubscriptionOperation{client}
}

// Invoke reads the named subscription.
func (operation GetSubscriptionOperation) Invoke(call sdkgo.Call, input GetSubscriptionInput) sdkgo.QueryAttempt[Subscription] {
	if !validSubscriptionName(input.Name) {
		return topicManagementQueryBranch(GetSubscriptionBranchDefect, Subscription{}, sdkgo.FailureValidation, "Pub/Sub subscription name is invalid")
	}
	response := operation.client.get(call, "/v1/"+input.Name)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementQueryFailure[Subscription](*failure, GetSubscriptionBranchDefect, GetSubscriptionBranchInvalidResponse)
	}
	if response.status == http.StatusNotFound {
		return topicManagementQueryBranch(GetSubscriptionBranchNotFound, Subscription{}, sdkgo.FailureNotFound, "Pub/Sub subscription was not found")
	}
	if response.status != http.StatusOK {
		return topicManagementQueryStatus[Subscription](response, GetSubscriptionBranchProviderRejected, "getSubscription")
	}
	subscription, isValid := decodeSubscription(response.body, input.Name)
	if !isValid {
		return topicManagementQueryBranch(GetSubscriptionBranchInvalidResponse, Subscription{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid subscription JSON")
	}
	return sdkgo.NewQueryBranch(GetSubscriptionBranchFound, subscription, nil, sdkgo.Receipt{})
}

// CreateSubscriptionOperation creates a pull subscription.
type CreateSubscriptionOperation struct{ client *Client }

// Definition returns the generated create contract.
func (CreateSubscriptionOperation) Definition() sdkgo.MutationDefinition {
	return CreateSubscriptionDefinition
}

// IdempotencyKey uses the logical call ID for the stable named create.
func (CreateSubscriptionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSubscriptionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// CreateSubscription binds the create Mutation to the client.
func (client *Client) CreateSubscription() CreateSubscriptionOperation {
	return CreateSubscriptionOperation{client}
}

// Invoke creates a pull subscription or confirms a matching existing one.
func (operation CreateSubscriptionOperation) Invoke(call sdkgo.Call, input CreateSubscriptionInput) sdkgo.MutationAttempt[Subscription] {
	if !validSubscriptionName(input.Name) || !validTopicName(input.Topic) || !validAckDeadline(input.AckDeadlineSeconds) || !validTopicLabels(input.Labels) {
		return topicManagementMutationBranch(CreateSubscriptionBranchDefect, Subscription{}, sdkgo.FailureValidation, "Pub/Sub subscription input is invalid")
	}
	body, _ := json.Marshal(struct {
		Topic              string            `json:"topic"`
		AckDeadlineSeconds int               `json:"ackDeadlineSeconds,omitempty"`
		Labels             map[string]string `json:"labels,omitempty"`
	}{input.Topic, input.AckDeadlineSeconds, input.Labels})
	response := operation.client.request(call, http.MethodPut, "/v1/"+input.Name, body)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementMutationFailure[Subscription](*failure, CreateSubscriptionBranchDefect, CreateSubscriptionBranchInvalidResponse)
	}
	if response.status == http.StatusConflict {
		read := operation.client.get(call, "/v1/"+input.Name)
		if read.err != nil || read.status >= 500 {
			return sdkgo.NewMutationRetry[Subscription](topicManagementFailure("createSubscription", sdkgo.FailureAvailability, "Pub/Sub subscription read-back failed"), 0)
		}
		if read.status == http.StatusOK {
			subscription, isValid := decodeSubscription(read.body, input.Name)
			if isValid && subscription.Topic == input.Topic && sameTopicLabels(subscription.Labels, input.Labels) && (input.AckDeadlineSeconds == 0 || subscription.AckDeadlineSeconds == input.AckDeadlineSeconds) {
				return sdkgo.NewMutationBranch(CreateSubscriptionBranchCreated, subscription, nil, topicManagementReceipt(call, subscription.Name))
			}
		}
		return topicManagementMutationBranch(CreateSubscriptionBranchProviderRejected, Subscription{}, sdkgo.FailureConflict, "Pub/Sub subscription already exists with different settings")
	}
	if response.status != http.StatusOK {
		return topicManagementMutationStatus[Subscription](response, CreateSubscriptionBranchProviderRejected, "createSubscription")
	}
	subscription, isValid := decodeSubscription(response.body, input.Name)
	if !isValid || subscription.Topic != input.Topic {
		return topicManagementMutationBranch(CreateSubscriptionBranchInvalidResponse, Subscription{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid created subscription JSON")
	}
	return sdkgo.NewMutationBranch(CreateSubscriptionBranchCreated, subscription, nil, topicManagementReceipt(call, subscription.Name))
}

// UpdateSubscriptionLabelsOperation replaces labels on one subscription.
type UpdateSubscriptionLabelsOperation struct{ client *Client }

// Definition returns the generated update contract.
func (UpdateSubscriptionLabelsOperation) Definition() sdkgo.MutationDefinition {
	return UpdateSubscriptionLabelsDefinition
}

// IdempotencyKey uses the logical call ID for an absolute replacement.
func (UpdateSubscriptionLabelsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSubscriptionLabelsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// UpdateSubscriptionLabels binds the update Mutation to the client.
func (client *Client) UpdateSubscriptionLabels() UpdateSubscriptionLabelsOperation {
	return UpdateSubscriptionLabelsOperation{client}
}

// Invoke replaces all subscription labels.
func (operation UpdateSubscriptionLabelsOperation) Invoke(call sdkgo.Call, input UpdateSubscriptionLabelsInput) sdkgo.MutationAttempt[Subscription] {
	if !validSubscriptionName(input.Name) || input.Labels == nil || !validTopicLabels(input.Labels) {
		return topicManagementMutationBranch(UpdateSubscriptionLabelsBranchDefect, Subscription{}, sdkgo.FailureValidation, "Pub/Sub subscription labels are invalid")
	}
	body, _ := json.Marshal(struct {
		Subscription Subscription `json:"subscription"`
		UpdateMask   string       `json:"updateMask"`
	}{Subscription: Subscription{Name: input.Name, Labels: input.Labels}, UpdateMask: "labels"})
	response := operation.client.request(call, http.MethodPatch, "/v1/"+input.Name, body)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementMutationFailure[Subscription](*failure, UpdateSubscriptionLabelsBranchDefect, UpdateSubscriptionLabelsBranchInvalidResponse)
	}
	if response.status == http.StatusNotFound {
		return topicManagementMutationBranch(UpdateSubscriptionLabelsBranchNotFound, Subscription{}, sdkgo.FailureNotFound, "Pub/Sub subscription was not found")
	}
	if response.status != http.StatusOK {
		return topicManagementMutationStatus[Subscription](response, UpdateSubscriptionLabelsBranchProviderRejected, "updateSubscriptionLabels")
	}
	subscription, isValid := decodeSubscription(response.body, input.Name)
	if !isValid || !sameTopicLabels(subscription.Labels, input.Labels) {
		return topicManagementMutationBranch(UpdateSubscriptionLabelsBranchInvalidResponse, Subscription{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid updated subscription JSON")
	}
	return sdkgo.NewMutationBranch(UpdateSubscriptionLabelsBranchUpdated, subscription, nil, topicManagementReceipt(call, subscription.Name))
}

// DeleteSubscriptionOperation removes one subscription.
type DeleteSubscriptionOperation struct{ client *Client }

// Definition returns the generated deletion contract.
func (DeleteSubscriptionOperation) Definition() sdkgo.MutationDefinition {
	return DeleteSubscriptionDefinition
}

// IdempotencyKey uses the logical call ID for a stable deletion.
func (DeleteSubscriptionOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSubscriptionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteSubscription binds the deletion Mutation to the client.
func (client *Client) DeleteSubscription() DeleteSubscriptionOperation {
	return DeleteSubscriptionOperation{client}
}

// Invoke deletes the subscription; absence is already deleted.
func (operation DeleteSubscriptionOperation) Invoke(call sdkgo.Call, input DeleteSubscriptionInput) sdkgo.MutationAttempt[DeletedSubscription] {
	if !validSubscriptionName(input.Name) {
		return topicManagementMutationBranch(DeleteSubscriptionBranchDefect, DeletedSubscription{}, sdkgo.FailureValidation, "Pub/Sub subscription name is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/v1/"+input.Name, nil)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementMutationFailure[DeletedSubscription](*failure, DeleteSubscriptionBranchDefect, DeleteSubscriptionBranchProviderRejected)
	}
	if response.status == http.StatusOK || response.status == http.StatusNoContent || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteSubscriptionBranchDeleted, DeletedSubscription{Name: input.Name}, nil, topicManagementReceipt(call, input.Name))
	}
	return topicManagementMutationStatus[DeletedSubscription](response, DeleteSubscriptionBranchProviderRejected, "deleteSubscription")
}

func validSubscriptionName(name string) bool { return subscriptionNamePattern.MatchString(name) }
func validAckDeadline(seconds int) bool      { return seconds == 0 || (seconds >= 10 && seconds <= 600) }
func validSubscription(subscription Subscription, name string) bool {
	return subscription.Name == name && validSubscriptionName(subscription.Name) && (validTopicName(subscription.Topic) || subscription.Topic == "_deleted-topic_") && validAckDeadline(subscription.AckDeadlineSeconds) && validTopicLabels(subscription.Labels)
}
func decodeSubscription(body []byte, name string) (Subscription, bool) {
	var subscription Subscription
	if json.Unmarshal(body, &subscription) != nil || !validSubscription(subscription, name) {
		return Subscription{}, false
	}
	return subscription, true
}
