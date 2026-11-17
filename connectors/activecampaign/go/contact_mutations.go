// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SyncContactInput creates or updates a contact identified by its email address.
type SyncContactInput struct {
	// Email is the contact's email address and the upsert identity.
	Email string `json:"email"`
	// FirstName replaces the given name when supplied; nil leaves it unchanged.
	FirstName *string `json:"firstName,omitempty"`
	// LastName replaces the family name when supplied; nil leaves it unchanged.
	LastName *string `json:"lastName,omitempty"`
}

// UpdateContactInput replaces selected contact fields by provider ID.
type UpdateContactInput struct {
	// ContactID is the numeric ID shown by ActiveCampaign's Contacts page.
	ContactID string `json:"contactId"`
	// Email replaces the email when supplied; nil leaves it unchanged.
	Email *string `json:"email,omitempty"`
	// FirstName replaces the given name when supplied; nil leaves it unchanged.
	FirstName *string `json:"firstName,omitempty"`
	// LastName replaces the family name when supplied; nil leaves it unchanged.
	LastName *string `json:"lastName,omitempty"`
}

// SyncContactOperation upserts one contact by email.
type SyncContactOperation struct{ client *Client }

// UpdateContactOperation replaces selected fields of one contact.
type UpdateContactOperation struct{ client *Client }

// SyncContact binds the upsert Mutation to this client.
func (client *Client) SyncContact() SyncContactOperation { return SyncContactOperation{client} }

// UpdateContact binds the update Mutation to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// Definition returns the manifest-generated upsert contract.
func (SyncContactOperation) Definition() sdkgo.MutationDefinition { return SyncContactDefinition }

// Definition returns the manifest-generated update contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// IdempotencyKey derives the stable call identity; the email upsert is safe to repeat.
func (SyncContactOperation) IdempotencyKey(callID sdkgo.CallID, _ SyncContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable call identity; absolute field values are safe to repeat.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke upserts the same field values on every attempt.
func (operation SyncContactOperation) Invoke(call sdkgo.Call, input SyncContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	if !validContactEmail(input.Email) {
		return contactMutationBranch("syncContact", SyncContactBranchDefect, sdkgo.FailureValidation, "email must be one valid address")
	}
	return invokeContactMutation(call, operation.client, "syncContact", http.MethodPost, "/contact/sync", map[string]any{
		"contact": contactFields(&input.Email, input.FirstName, input.LastName),
	}, SyncContactBranchSynced, "", SyncContactBranchProviderRejected, SyncContactBranchInvalidResponse, SyncContactBranchDefect)
}

// Invoke sets only the supplied absolute field values on every attempt.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	if !numericID.MatchString(input.ContactID) || input.Email == nil && input.FirstName == nil && input.LastName == nil {
		return contactMutationBranch("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "numeric contact ID and at least one field are required")
	}
	if input.Email != nil && !validContactEmail(*input.Email) {
		return contactMutationBranch("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "email must be one valid address")
	}
	return invokeContactMutation(call, operation.client, "updateContact", http.MethodPut, "/contacts/"+input.ContactID, map[string]any{
		"contact": contactFields(input.Email, input.FirstName, input.LastName),
	}, UpdateContactBranchUpdated, UpdateContactBranchNotFound, UpdateContactBranchProviderRejected, UpdateContactBranchInvalidResponse, UpdateContactBranchDefect)
}

func validContactEmail(email string) bool {
	if strings.TrimSpace(email) != email || email == "" {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}

func contactFields(email, firstName, lastName *string) map[string]string {
	fields := make(map[string]string, 3)
	if email != nil {
		fields["email"] = *email
	}
	if firstName != nil {
		fields["firstName"] = *firstName
	}
	if lastName != nil {
		fields["lastName"] = *lastName
	}
	return fields
}

func invokeContactMutation(call sdkgo.Call, client *Client, operation, method, path string, payload any, success, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[ContactMetadata] {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return contactMutationBranch(operation, defect, sdkgo.FailureLocalDefect, "contact request could not be encoded")
	}
	body, status, retryAfter, err := client.request(call, method, path, encoded)
	if errors.Is(err, errCredential) {
		return contactMutationBranch(operation, defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure(operation, sdkgo.FailureTransport, "ActiveCampaign request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure(operation, sdkgo.FailureRateLimit, "ActiveCampaign rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure(operation, sdkgo.FailureAvailability, "ActiveCampaign is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound && notFound != "" {
		return contactMutationBranch(operation, notFound, sdkgo.FailureNotFound, "ActiveCampaign did not find the contact")
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return contactMutationBranch(operation, rejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return contactMutationBranch(operation, invalid, sdkgo.FailureResponseTooLarge, "ActiveCampaign response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure(operation, sdkgo.FailureTransport, "ActiveCampaign response was interrupted"), 0)
	}
	contact, err := decodeContact(body)
	if err != nil {
		return contactMutationBranch(operation, invalid, sdkgo.FailureProtocol, "ActiveCampaign returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(success, contact, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "activecampaign", ProviderObjectID: contact.ID, ObservedAt: time.Now().UTC()})
}

func contactMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContactMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ContactMetadata{}, &failure, sdkgo.Receipt{})
}
