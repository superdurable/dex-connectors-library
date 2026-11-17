// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListContactsInput selects a one-based page from a Freshsales contact view.
type ListContactsInput struct {
	ViewID string `json:"viewId"`
	Page   int    `json:"page,omitempty"`
}

// ContactsPage contains one page of contact identities and the provider total.
type ContactsPage struct {
	Contacts []ContactMetadata `json:"contacts"`
	Page     int               `json:"page"`
	Total    int               `json:"total"`
}

// CreateContactInput contains the contact names and optional email.
type CreateContactInput struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	EmailAddress string `json:"emailAddress,omitempty"`
}

// UpdateContactInput replaces a contact's first and last names.
type UpdateContactInput struct {
	ContactID string `json:"contactId"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// DeleteContactInput identifies the contact to remove.
type DeleteContactInput struct {
	ContactID string `json:"contactId"`
}

// DeletedContact confirms that a contact ID is absent.
type DeletedContact struct {
	ContactID string `json:"contactId"`
	Deleted   bool   `json:"deleted"`
}

// ListContactsOperation reads one contact view page.
type ListContactsOperation struct{ client *Client }

// CreateContactOperation creates a contact with bounded sends.
type CreateContactOperation struct{ client *Client }

// UpdateContactOperation replaces contact names.
type UpdateContactOperation struct{ client *Client }

// DeleteContactOperation removes one contact.
type DeleteContactOperation struct{ client *Client }

// ListContacts binds the query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// CreateContact binds the mutation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// UpdateContact binds the mutation to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// DeleteContact binds the mutation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Definition returns the manifest-generated create contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// Definition returns the manifest-generated update contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// Definition returns the manifest-generated delete contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey correlates a Dex call; Freshsales does not deduplicate this create.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a Dex call; repeated replacement is harmless.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a Dex call; repeated deletion is harmless.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one page from the chosen contact view.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	branches := queryBranches{found: ListContactsBranchListed, notFound: ListContactsBranchNotFound, rejected: ListContactsBranchProviderRejected, invalid: ListContactsBranchInvalidResponse, defect: ListContactsBranchDefect}
	if input.Page == 0 {
		input.Page = 1
	}
	if !isPositiveContactID(input.ViewID) || input.Page < 1 || input.Page > 10000 {
		return queryBranch("listContacts", branches.defect, ContactsPage{}, sdkgo.FailureValidation, "view ID or page is invalid")
	}
	return invokeQuery(call, operation.client, "listContacts", "/contacts/view/"+input.ViewID+"?page="+strconv.Itoa(input.Page), branches, func(body []byte) (ContactsPage, error) { return decodeContactsPage(body, input.Page) })
}

// Invoke creates a contact with at most call.MaxSends possible sends.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	if strings.TrimSpace(input.LastName) == "" || len(input.LastName) > 255 || len(input.FirstName) > 255 || len(input.EmailAddress) > 320 {
		return createBranch[ContactMetadata]("createContact", CreateContactBranchDefect, sdkgo.FailureValidation, "contact name or email is invalid")
	}
	requestBody, err := json.Marshal(map[string]any{"contact": map[string]string{"first_name": input.FirstName, "last_name": input.LastName, "email": input.EmailAddress}})
	if err != nil {
		return createBranch[ContactMetadata]("createContact", CreateContactBranchDefect, sdkgo.FailureLocalDefect, "contact request cannot be encoded")
	}
	return invokeLimitedCreate(operation.client, call, "createContact", "/contacts", requestBody, CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, createBranches{CreateContactBranchCreated, CreateContactBranchProviderRejected, CreateContactBranchDefect}, decodeContact, func(contact ContactMetadata) string { return strconv.FormatInt(contact.ID, 10) }, func(id string) string { return "/contacts/" + id })
}

// Invoke replaces contact names and confirms the provider response.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	if !isPositiveContactID(input.ContactID) || strings.TrimSpace(input.LastName) == "" || len(input.LastName) > 255 || len(input.FirstName) > 255 {
		return contactChangeBranch(UpdateContactBranchDefect, sdkgo.FailureValidation, "contact ID or name is invalid")
	}
	body, status, delay, err := operation.client.requestJSON(call, call.Context, http.MethodPut, "/contacts/"+input.ContactID, map[string]any{"contact": map[string]string{"first_name": input.FirstName, "last_name": input.LastName}})
	if attempt := classifyContactChange(status, delay, err); attempt != nil {
		return *attempt
	}
	contact, decodeErr := decodeContact(body)
	if decodeErr != nil || strconv.FormatInt(contact.ID, 10) != input.ContactID {
		return contactChangeRetry(sdkgo.FailureProtocol, "contact update answer is unconfirmed", 0)
	}
	confirmedBody, status, delay, err := operation.client.get(call, "/contacts/"+input.ContactID)
	if attempt := classifyContactChange(status, delay, err); attempt != nil {
		return *attempt
	}
	confirmed, decodeErr := decodeContact(confirmedBody)
	if decodeErr != nil || confirmed.FirstName != input.FirstName || confirmed.LastName != input.LastName {
		return contactChangeRetry(sdkgo.FailureProtocol, "contact names are not yet confirmed", 0)
	}
	return sdkgo.NewMutationBranch(UpdateContactBranchUpdated, confirmed, nil, createReceipt(call, input.ContactID))
}

// Invoke deletes a contact; a missing contact is already in the requested state.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeletedContact] {
	if !isPositiveContactID(input.ContactID) {
		return deletedContactBranch(DeleteContactBranchDefect, sdkgo.FailureValidation, "contact ID is invalid")
	}
	body, status, delay, err := operation.client.request(call, call.Context, http.MethodDelete, "/contacts/"+input.ContactID, nil)
	switch {
	case errors.Is(err, errCredential):
		return deletedContactBranch(DeleteContactBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable or invalid")
	case status == http.StatusTooManyRequests:
		return deletedContactRetry(sdkgo.FailureRateLimit, "Freshsales rate limited deletion", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		return deletedContactRetry(sdkgo.FailureAvailability, "contact deletion outcome unconfirmed", delay)
	case status == http.StatusNotFound || status == http.StatusNoContent:
		return deletedContactSuccess(call, input.ContactID)
	case status != http.StatusOK:
		return deletedContactBranch(DeleteContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Freshsales rejected HTTP "+strconv.Itoa(status))
	case err != nil:
		return deletedContactRetry(sdkgo.FailureTransport, "contact deletion response interrupted", 0)
	}
	var deleted bool
	if json.Unmarshal(body, &deleted) != nil || !deleted {
		return deletedContactRetry(sdkgo.FailureProtocol, "contact deletion answer unconfirmed", 0)
	}
	return deletedContactSuccess(call, input.ContactID)
}

func decodeContactsPage(body []byte, pageNumber int) (ContactsPage, error) {
	var wire struct {
		Contacts []json.RawMessage `json:"contacts"`
		Meta     struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactsPage{}, err
	}
	if wire.Contacts == nil || wire.Meta.Total < 0 {
		return ContactsPage{}, fmt.Errorf("contact page missing")
	}
	page := ContactsPage{Contacts: make([]ContactMetadata, 0, len(wire.Contacts)), Page: pageNumber, Total: wire.Meta.Total}
	for _, raw := range wire.Contacts {
		wrapped, err := json.Marshal(map[string]any{"contact": json.RawMessage(raw)})
		if err != nil {
			return ContactsPage{}, err
		}
		contact, err := decodeContact(wrapped)
		if err != nil {
			return ContactsPage{}, err
		}
		page.Contacts = append(page.Contacts, contact)
	}
	return page, nil
}

func classifyContactChange(status int, delay time.Duration, err error) *sdkgo.MutationAttempt[ContactMetadata] {
	var attempt sdkgo.MutationAttempt[ContactMetadata]
	switch {
	case errors.Is(err, errCredential):
		attempt = contactChangeBranch(UpdateContactBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable or invalid")
	case status == http.StatusTooManyRequests:
		attempt = contactChangeRetry(sdkgo.FailureRateLimit, "Freshsales rate limited contact update", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		attempt = contactChangeRetry(sdkgo.FailureAvailability, "contact update outcome unconfirmed", delay)
	case status == http.StatusNotFound:
		attempt = contactChangeBranch(UpdateContactBranchNotFound, sdkgo.FailureNotFound, "Freshsales did not find contact")
	case status != http.StatusOK:
		attempt = contactChangeBranch(UpdateContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Freshsales rejected HTTP "+strconv.Itoa(status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		attempt = contactChangeRetry(sdkgo.FailureResponseTooLarge, "contact update response exceeded limit", 0)
	case err != nil:
		attempt = contactChangeRetry(sdkgo.FailureTransport, "contact update response interrupted", 0)
	default:
		return nil
	}
	return &attempt
}
func contactChangeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContactMetadata] {
	failure := queryFailure("updateContact", kind, message)
	return sdkgo.NewMutationBranch(branch, ContactMetadata{}, &failure, sdkgo.Receipt{})
}
func contactChangeRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[ContactMetadata] {
	return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("updateContact", kind, message), delay)
}
func deletedContactSuccess(call sdkgo.Call, contactID string) sdkgo.MutationAttempt[DeletedContact] {
	return sdkgo.NewMutationBranch(DeleteContactBranchDeleted, DeletedContact{ContactID: contactID, Deleted: true}, nil, createReceipt(call, contactID))
}
func deletedContactBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedContact] {
	failure := queryFailure("deleteContact", kind, message)
	return sdkgo.NewMutationBranch(branch, DeletedContact{}, &failure, sdkgo.Receipt{})
}
func deletedContactRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[DeletedContact] {
	return sdkgo.NewMutationRetry[DeletedContact](queryFailure("deleteContact", kind, message), delay)
}
