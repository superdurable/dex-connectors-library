// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListContactsInput selects a bounded page of CRM contacts.
type ListContactsInput struct {
	// PageSize is the maximum contacts to return; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the opaque token returned by a prior page.
	PageToken string `json:"pageToken,omitempty"`
}

// ContactsPage contains contact identities and a possible continuation token.
type ContactsPage struct {
	// Contacts are the returned contact identities and names.
	Contacts []ContactProfile `json:"contacts"`
	// NextPageToken selects the following page, when present.
	NextPageToken string `json:"nextPageToken"`
}

// UpsertContactInput supplies the email identity and names to replace.
type UpsertContactInput struct {
	// Email is the non-secret contact email used for Keap duplicate matching.
	Email string `json:"email"`
	// GivenName is the contact's replacement first name.
	GivenName string `json:"givenName"`
	// FamilyName is the contact's replacement surname.
	FamilyName string `json:"familyName"`
}

// ContactDeletion confirms a contact deletion.
type ContactDeletion struct {
	// ContactID is the deleted provider contact ID.
	ContactID string `json:"contactId"`
	// Deleted reports Keap's acceptance of the deletion.
	Deleted bool `json:"deleted"`
}

// ListContactsOperation lists CRM contacts.
type ListContactsOperation struct{ client *Client }

// UpsertContactOperation creates or updates a contact by email.
type UpsertContactOperation struct{ client *Client }

// DeleteContactOperation removes one CRM contact.
type DeleteContactOperation struct{ client *Client }

// ListContacts binds the query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// UpsertContact binds the mutation to this client.
func (client *Client) UpsertContact() UpsertContactOperation { return UpsertContactOperation{client} }

// DeleteContact binds the mutation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the generated query contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Definition returns the generated mutation contract.
func (UpsertContactOperation) Definition() sdkgo.MutationDefinition { return UpsertContactDefinition }

// Definition returns the generated mutation contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey derives a stable call identity across retries.
func (UpsertContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call identity across retries.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ GetContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads a page, preserving only contact IDs and names.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	if input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return keapQueryBranch("listContacts", ListContactsBranchDefect, ContactsPage{}, sdkgo.FailureValidation, "page size or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"fields": {"id,given_name,family_name"}, "page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/contacts?"+query.Encode(), nil)
	if branch, retry, handled := classifyKeapQuery(response, "listContacts", ListContactsBranchProviderRejected, ListContactsBranchInvalidResponse, ListContactsBranchDefect, ContactsPage{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	var wire struct {
		Contacts []struct {
			ID         string `json:"id"`
			GivenName  string `json:"given_name"`
			FamilyName string `json:"family_name"`
		} `json:"contacts"`
		NextPageToken string `json:"next_page_token"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Contacts == nil {
		return keapQueryBranch("listContacts", ListContactsBranchInvalidResponse, ContactsPage{}, sdkgo.FailureProtocol, "Keap returned invalid contact list JSON")
	}
	page := ContactsPage{Contacts: make([]ContactProfile, 0, len(wire.Contacts)), NextPageToken: wire.NextPageToken}
	for _, contact := range wire.Contacts {
		if !validContactID(contact.ID) {
			return keapQueryBranch("listContacts", ListContactsBranchInvalidResponse, ContactsPage{}, sdkgo.FailureProtocol, "Keap returned invalid contact identity")
		}
		page.Contacts = append(page.Contacts, ContactProfile{ID: contact.ID, GivenName: contact.GivenName, FamilyName: contact.FamilyName})
	}
	return sdkgo.NewQueryBranch(ListContactsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke upserts a contact using Keap's email duplicate matching.
func (operation UpsertContactOperation) Invoke(call sdkgo.Call, input UpsertContactInput) sdkgo.MutationAttempt[ContactProfile] {
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 75 || len(input.GivenName) > 128 || len(input.FamilyName) > 128 {
		return keapMutationBranch("upsertContact", UpsertContactBranchDefect, ContactProfile{}, sdkgo.FailureValidation, "valid email and bounded names are required")
	}
	body, err := json.Marshal(struct {
		EmailAddresses []struct {
			Email string `json:"email"`
			Field string `json:"field"`
		} `json:"email_addresses"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
	}{EmailAddresses: []struct {
		Email string `json:"email"`
		Field string `json:"field"`
	}{{Email: input.Email, Field: "EMAIL1"}}, GivenName: input.GivenName, FamilyName: input.FamilyName})
	if err != nil {
		return keapMutationBranch("upsertContact", UpsertContactBranchDefect, ContactProfile{}, sdkgo.FailureValidation, "contact input cannot be encoded")
	}
	response := operation.client.request(call, http.MethodPost, "/rest/v2/contacts?duplicate_option=Email&fields=id%2Cgiven_name%2Cfamily_name", body)
	if branch, retry, handled := classifyKeapMutation(response, "upsertContact", UpsertContactBranchProviderRejected, UpsertContactBranchInvalidResponse, UpsertContactBranchDefect, ContactProfile{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return keapMutationBranch("upsertContact", UpsertContactBranchProviderRejected, ContactProfile{}, sdkgo.FailureProviderRejection, "Keap rejected contact upsert")
	}
	var wire struct {
		ID         string `json:"id"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || !validContactID(wire.ID) {
		return keapMutationBranch("upsertContact", UpsertContactBranchInvalidResponse, ContactProfile{}, sdkgo.FailureProtocol, "Keap returned invalid contact JSON")
	}
	return sdkgo.NewMutationBranch(UpsertContactBranchUpserted, ContactProfile{ID: wire.ID, GivenName: wire.GivenName, FamilyName: wire.FamilyName}, nil, sdkgo.Receipt{})
}

// Invoke deletes one contact by ID.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.MutationAttempt[ContactDeletion] {
	if !validContactID(input.ContactID) {
		return keapMutationBranch("deleteContact", DeleteContactBranchDefect, ContactDeletion{}, sdkgo.FailureValidation, "contact ID is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/rest/v2/contacts/"+input.ContactID, nil)
	if response.status == http.StatusNotFound {
		return keapMutationBranch("deleteContact", DeleteContactBranchNotFound, ContactDeletion{}, sdkgo.FailureNotFound, "Keap did not find the contact")
	}
	if branch, retry, handled := classifyKeapMutation(response, "deleteContact", DeleteContactBranchProviderRejected, DeleteContactBranchInvalidResponse, DeleteContactBranchDefect, ContactDeletion{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != http.StatusNoContent && response.status != http.StatusOK {
		return keapMutationBranch("deleteContact", DeleteContactBranchProviderRejected, ContactDeletion{}, sdkgo.FailureProviderRejection, "Keap rejected contact deletion")
	}
	if response.status == http.StatusOK && len(response.body) > 0 && !json.Valid(response.body) {
		return keapMutationBranch("deleteContact", DeleteContactBranchInvalidResponse, ContactDeletion{}, sdkgo.FailureProtocol, "Keap returned invalid deletion JSON")
	}
	return sdkgo.NewMutationBranch(DeleteContactBranchDeleted, ContactDeletion{ContactID: input.ContactID, Deleted: true}, nil, sdkgo.Receipt{})
}

func classifyKeapQuery[T any](response providerResponse, operation string, rejected, invalid, defect sdkgo.BranchID, zero T) (*sdkgo.QueryAttempt[T], *sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		result := keapQueryBranch(operation, defect, zero, sdkgo.FailureAuthentication, "Keap credentials are unavailable")
		return &result, nil, true
	}
	if errors.Is(response.err, errRequest) {
		result := sdkgo.NewQueryRetry[T](keapFailure(operation, sdkgo.FailureTransport, "Keap request failed"), 0)
		return nil, &result, true
	}
	if response.status == 429 || response.status == 408 || response.status >= 500 {
		result := sdkgo.NewQueryRetry[T](keapFailure(operation, sdkgo.FailureAvailability, "Keap temporarily unavailable"), response.retryAfter)
		return nil, &result, true
	}
	if response.status != 200 {
		result := keapQueryBranch(operation, rejected, zero, sdkgo.FailureProviderRejection, "Keap rejected HTTP "+strconv.Itoa(response.status))
		return &result, nil, true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		result := keapQueryBranch(operation, invalid, zero, sdkgo.FailureResponseTooLarge, "Keap response exceeded configured limit")
		return &result, nil, true
	}
	if response.err != nil {
		result := sdkgo.NewQueryRetry[T](keapFailure(operation, sdkgo.FailureTransport, "Keap response was interrupted"), 0)
		return nil, &result, true
	}
	return nil, nil, false
}

func classifyKeapMutation[T any](response providerResponse, operation string, rejected, invalid, defect sdkgo.BranchID, zero T) (*sdkgo.MutationAttempt[T], *sdkgo.MutationAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		result := keapMutationBranch(operation, defect, zero, sdkgo.FailureAuthentication, "Keap credentials are unavailable")
		return &result, nil, true
	}
	if errors.Is(response.err, errRequest) {
		result := sdkgo.NewMutationRetry[T](keapFailure(operation, sdkgo.FailureTransport, "Keap request failed"), 0)
		return nil, &result, true
	}
	if response.status == 429 || response.status == 408 || response.status >= 500 {
		result := sdkgo.NewMutationRetry[T](keapFailure(operation, sdkgo.FailureAvailability, "Keap temporarily unavailable"), response.retryAfter)
		return nil, &result, true
	}
	if response.status >= 400 && response.status != 404 {
		result := keapMutationBranch(operation, rejected, zero, sdkgo.FailureProviderRejection, "Keap rejected HTTP "+strconv.Itoa(response.status))
		return &result, nil, true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		result := keapMutationBranch(operation, invalid, zero, sdkgo.FailureResponseTooLarge, "Keap response exceeded configured limit")
		return &result, nil, true
	}
	if response.err != nil {
		result := sdkgo.NewMutationRetry[T](keapFailure(operation, sdkgo.FailureTransport, "Keap response was interrupted"), 0)
		return nil, &result, true
	}
	return nil, nil, false
}

func keapFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "keap", Operation: operation, Message: message}
}

func keapQueryBranch[T any](operation string, branch sdkgo.BranchID, zero T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := keapFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func keapMutationBranch[T any](operation string, branch sdkgo.BranchID, zero T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := keapFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}
