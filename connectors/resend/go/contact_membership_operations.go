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

// ContactSegmentInput identifies a contact and segment membership.
type ContactSegmentInput struct {
	// ContactID is the contact ID or email address.
	ContactID string `json:"contactId"`
	// SegmentID is the segment ID.
	SegmentID string `json:"segmentId"`
}

// ListContactSegmentsInput selects a contact's segment page.
type ListContactSegmentsInput struct {
	// ContactID is the contact ID or email address.
	ContactID string `json:"contactId"`
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive segment ID cursor.
	After string `json:"after,omitempty"`
}

// GetContactTopicsInput selects a contact's topic subscriptions.
type GetContactTopicsInput struct {
	// ContactID is the contact ID or email address.
	ContactID string `json:"contactId"`
	// Limit is the page size, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive topic ID cursor.
	After string `json:"after,omitempty"`
}

// UpdateContactTopicsInput sets absolute topic subscriptions for a contact.
type UpdateContactTopicsInput struct {
	// ContactID is the contact ID or email address.
	ContactID string `json:"contactId"`
	// Topics contains topic IDs and opt_in or opt_out subscription choices.
	Topics []ContactTopicSubscription `json:"topics"`
}

// ContactSegmentReference identifies an added segment membership.
type ContactSegmentReference struct {
	// ContactID is the provider contact ID.
	ContactID string `json:"contact_id"`
	// SegmentID is the provider segment ID.
	SegmentID string `json:"segment_id"`
}

// RemovedContactSegment records a removed segment membership.
type RemovedContactSegment struct {
	// ContactID is the provider contact ID.
	ContactID string `json:"contact_id"`
	// SegmentID is the provider segment ID.
	SegmentID string `json:"segment_id"`
	// Deleted confirms that removal succeeded.
	Deleted bool `json:"deleted"`
}

// ContactTopicSubscription describes a contact's topic choice.
type ContactTopicSubscription struct {
	// ID is the topic ID.
	ID string `json:"id"`
	// Subscription is opt_in or opt_out.
	Subscription string `json:"subscription"`
}

// ContactTopic contains a topic's name and a contact's subscription choice.
type ContactTopic struct {
	// ID is the topic ID.
	ID string `json:"id"`
	// Name is the topic name.
	Name string `json:"name"`
	// Description is optional topic text.
	Description string `json:"description"`
	// Subscription is opt_in or opt_out.
	Subscription string `json:"subscription"`
}

// ContactTopicPage contains one page of a contact's topic subscriptions.
type ContactTopicPage struct {
	// Data contains topic subscription entries.
	Data []ContactTopic `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// UpdatedContactTopics confirms a contact's topic subscriptions.
type UpdatedContactTopics struct {
	// ContactID is the provider contact ID.
	ContactID string `json:"contact_id"`
	// Topics contains confirmed topic subscriptions.
	Topics []ContactTopicSubscription `json:"topics"`
}

// AddContactToSegmentOperation sets one membership.
type AddContactToSegmentOperation struct{ client *Client }

// RemoveContactFromSegmentOperation removes one membership.
type RemoveContactFromSegmentOperation struct{ client *Client }

// ListContactSegmentsOperation lists a contact's segments.
type ListContactSegmentsOperation struct{ client *Client }

// GetContactTopicsOperation retrieves a contact's topic subscriptions.
type GetContactTopicsOperation struct{ client *Client }

// UpdateContactTopicsOperation sets topic subscriptions.
type UpdateContactTopicsOperation struct{ client *Client }

// AddContactToSegment binds membership addition to this client.
func (client *Client) AddContactToSegment() AddContactToSegmentOperation {
	return AddContactToSegmentOperation{client}
}

// RemoveContactFromSegment binds membership removal to this client.
func (client *Client) RemoveContactFromSegment() RemoveContactFromSegmentOperation {
	return RemoveContactFromSegmentOperation{client}
}

// ListContactSegments binds membership listing to this client.
func (client *Client) ListContactSegments() ListContactSegmentsOperation {
	return ListContactSegmentsOperation{client}
}

// GetContactTopics binds subscription retrieval to this client.
func (client *Client) GetContactTopics() GetContactTopicsOperation {
	return GetContactTopicsOperation{client}
}

// UpdateContactTopics binds subscription update to this client.
func (client *Client) UpdateContactTopics() UpdateContactTopicsOperation {
	return UpdateContactTopicsOperation{client}
}

// Definition returns the generated addContactToSegment contract.
func (AddContactToSegmentOperation) Definition() sdkgo.MutationDefinition {
	return AddContactToSegmentDefinition
}

// Definition returns the generated removeContactFromSegment contract.
func (RemoveContactFromSegmentOperation) Definition() sdkgo.MutationDefinition {
	return RemoveContactFromSegmentDefinition
}

// Definition returns the generated listContactSegments contract.
func (ListContactSegmentsOperation) Definition() sdkgo.QueryDefinition {
	return ListContactSegmentsDefinition
}

// Definition returns the generated getContactTopics contract.
func (GetContactTopicsOperation) Definition() sdkgo.QueryDefinition {
	return GetContactTopicsDefinition
}

// Definition returns the generated updateContactTopics contract.
func (UpdateContactTopicsOperation) Definition() sdkgo.MutationDefinition {
	return UpdateContactTopicsDefinition
}

// IdempotencyKey correlates set-membership attempts.
func (AddContactToSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ ContactSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates removal attempts.
func (RemoveContactFromSegmentOperation) IdempotencyKey(callID sdkgo.CallID, _ ContactSegmentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute subscription changes.
func (UpdateContactTopicsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactTopicsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds one contact to a segment.
func (operation AddContactToSegmentOperation) Invoke(call sdkgo.Call, input ContactSegmentInput) sdkgo.MutationAttempt[ContactSegmentReference] {
	if !validContactSegmentInput(input) {
		return mutationFailure[ContactSegmentReference]("addContactToSegment", AddContactToSegmentBranchDefect, sdkgo.FailureValidation, "contact and segment IDs are required")
	}
	return runMutation(operation.client, call, "addContactToSegment", http.MethodPost, contactSegmentPath(input), nil, "", "", AddContactToSegmentBranchAdded, AddContactToSegmentBranchNotFound, AddContactToSegmentBranchProviderRejected, AddContactToSegmentBranchInvalidResponse, AddContactToSegmentBranchDefect, decodeContactSegmentReference)
}

// Invoke removes one contact from a segment.
func (operation RemoveContactFromSegmentOperation) Invoke(call sdkgo.Call, input ContactSegmentInput) sdkgo.MutationAttempt[RemovedContactSegment] {
	if !validContactSegmentInput(input) {
		return mutationFailure[RemovedContactSegment]("removeContactFromSegment", RemoveContactFromSegmentBranchDefect, sdkgo.FailureValidation, "contact and segment IDs are required")
	}
	return runMutation(operation.client, call, "removeContactFromSegment", http.MethodDelete, contactSegmentPath(input), nil, "", "", RemoveContactFromSegmentBranchRemoved, RemoveContactFromSegmentBranchNotFound, RemoveContactFromSegmentBranchProviderRejected, RemoveContactFromSegmentBranchInvalidResponse, RemoveContactFromSegmentBranchDefect, decodeRemovedContactSegment)
}

// Invoke lists a contact's segments.
func (operation ListContactSegmentsOperation) Invoke(call sdkgo.Call, input ListContactSegmentsInput) sdkgo.QueryAttempt[SegmentPage] {
	if strings.TrimSpace(input.ContactID) == "" {
		return queryFailure[SegmentPage]("listContactSegments", ListContactSegmentsBranchDefect, sdkgo.FailureValidation, "contact ID or email is required")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[SegmentPage]("listContactSegments", ListContactSegmentsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listContactSegments", "/contacts/"+url.PathEscape(input.ContactID)+"/segments"+query, ListContactSegmentsBranchListed, "", ListContactSegmentsBranchProviderRejected, ListContactSegmentsBranchInvalidResponse, ListContactSegmentsBranchDefect, decodeSegmentPage)
}

// Invoke retrieves a contact's topic subscriptions.
func (operation GetContactTopicsOperation) Invoke(call sdkgo.Call, input GetContactTopicsInput) sdkgo.QueryAttempt[ContactTopicPage] {
	if strings.TrimSpace(input.ContactID) == "" {
		return queryFailure[ContactTopicPage]("getContactTopics", GetContactTopicsBranchDefect, sdkgo.FailureValidation, "contact ID or email is required")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[ContactTopicPage]("getContactTopics", GetContactTopicsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "getContactTopics", "/contacts/"+url.PathEscape(input.ContactID)+"/topics"+query, GetContactTopicsBranchFound, GetContactTopicsBranchNotFound, GetContactTopicsBranchProviderRejected, GetContactTopicsBranchInvalidResponse, GetContactTopicsBranchDefect, decodeContactTopicPage)
}

// Invoke sets topic subscription choices on a contact.
func (operation UpdateContactTopicsOperation) Invoke(call sdkgo.Call, input UpdateContactTopicsInput) sdkgo.MutationAttempt[UpdatedContactTopics] {
	if strings.TrimSpace(input.ContactID) == "" {
		return mutationFailure[UpdatedContactTopics]("updateContactTopics", UpdateContactTopicsBranchDefect, sdkgo.FailureValidation, "contact ID or email is required")
	}
	for _, topic := range input.Topics {
		if strings.TrimSpace(topic.ID) == "" || (topic.Subscription != "opt_in" && topic.Subscription != "opt_out") {
			return mutationFailure[UpdatedContactTopics]("updateContactTopics", UpdateContactTopicsBranchDefect, sdkgo.FailureValidation, "topic ID or subscription is invalid")
		}
	}
	topics := input.Topics
	if topics == nil {
		topics = []ContactTopicSubscription{}
	}
	body, err := json.Marshal(map[string]any{"topics": topics})
	if err != nil {
		return mutationFailure[UpdatedContactTopics]("updateContactTopics", UpdateContactTopicsBranchDefect, sdkgo.FailureValidation, "topics cannot be encoded")
	}
	return runMutation(operation.client, call, "updateContactTopics", http.MethodPatch, "/contacts/"+url.PathEscape(input.ContactID)+"/topics", body, "", "", UpdateContactTopicsBranchUpdated, UpdateContactTopicsBranchNotFound, UpdateContactTopicsBranchProviderRejected, UpdateContactTopicsBranchInvalidResponse, UpdateContactTopicsBranchDefect, decodeUpdatedContactTopics)
}
func validContactSegmentInput(input ContactSegmentInput) bool {
	return strings.TrimSpace(input.ContactID) != "" && strings.TrimSpace(input.SegmentID) != ""
}
func contactSegmentPath(input ContactSegmentInput) string {
	return "/contacts/" + url.PathEscape(input.ContactID) + "/segments/" + url.PathEscape(input.SegmentID)
}
func decodeContactSegmentReference(body []byte) (ContactSegmentReference, error) {
	var value ContactSegmentReference
	err := json.Unmarshal(body, &value)
	if err == nil && (value.ContactID == "" || value.SegmentID == "") {
		err = fmt.Errorf("contact segment IDs missing")
	}
	return value, err
}
func decodeRemovedContactSegment(body []byte) (RemovedContactSegment, error) {
	var value RemovedContactSegment
	err := json.Unmarshal(body, &value)
	if err == nil && (!value.Deleted || value.ContactID == "" || value.SegmentID == "") {
		err = fmt.Errorf("contact segment removal acknowledgement missing")
	}
	return value, err
}
func decodeContactTopicPage(body []byte) (ContactTopicPage, error) {
	var page ContactTopicPage
	err := json.Unmarshal(body, &page)
	if err == nil && page.Data == nil {
		err = fmt.Errorf("contact topic page data missing")
	}
	return page, err
}
func decodeUpdatedContactTopics(body []byte) (UpdatedContactTopics, error) {
	var value UpdatedContactTopics
	err := json.Unmarshal(body, &value)
	if err == nil && (value.ContactID == "" || value.Topics == nil) {
		err = fmt.Errorf("updated topic subscriptions missing")
	}
	return value, err
}
