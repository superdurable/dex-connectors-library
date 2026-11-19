// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactsInput selects a page of Harvest client contacts.
type ListContactsInput struct {
	// ClientID optionally restricts results to one client.
	ClientID int64 `json:"clientId,omitempty"`
	// Cursor is the opaque cursor returned by the prior page.
	Cursor string `json:"cursor,omitempty"`
	// PerPage is from 1 to 2000; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
}

// GetContactInput identifies a Harvest contact.
type GetContactInput struct {
	ID int64 `json:"id"`
}

// CreateContactInput creates a contact under a Harvest client.
type CreateContactInput struct {
	// ClientID is the positive owner client ID.
	ClientID int64 `json:"clientId"`
	// FirstName is the contact's given name.
	FirstName string `json:"firstName"`
	// LastName is the contact's family name.
	LastName string `json:"lastName"`
	// Email is an optional email address.
	Email string `json:"email,omitempty"`
}

// UpdateContactInput replaces supplied contact fields.
type UpdateContactInput struct {
	// ID is the positive contact ID.
	ID int64 `json:"id"`
	// FirstName replaces the given name when supplied.
	FirstName *string `json:"firstName,omitempty"`
	// LastName replaces the family name when supplied.
	LastName *string `json:"lastName,omitempty"`
	// Email replaces the email when supplied; blank clears it.
	Email *string `json:"email,omitempty"`
}

// DeleteContactInput identifies a contact to delete.
type DeleteContactInput struct {
	ID int64 `json:"id"`
}

// ContactRecord is a Harvest client contact.
type ContactRecord struct {
	// ID is assigned by Harvest.
	ID int64 `json:"id"`
	// ClientID identifies the owning client.
	ClientID int64 `json:"clientId"`
	// FirstName is the contact's given name.
	FirstName string `json:"firstName"`
	// LastName is the contact's family name.
	LastName string `json:"lastName"`
	// Email is the optional contact email.
	Email string `json:"email,omitempty"`
}

// ContactPage contains one page of contacts and its next cursor.
type ContactPage struct {
	// Contacts are the records in this page.
	Contacts []ContactRecord `json:"contacts"`
	// NextCursor is blank for the final page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// DeletedContact identifies a deleted contact.
type DeletedContact struct {
	ID int64 `json:"id"`
}

// ListContactsOperation reads Harvest client contacts.
type ListContactsOperation struct{ client *Client }

// ListContacts binds the contact-list Query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// Definition returns the generated Query contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Invoke reads one contact page.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactPage] {
	if input.ClientID < 0 {
		return failedBranch[ContactPage]("listContacts", ListContactsBranchDefect, sdkgo.FailureValidation, "client ID must not be negative")
	}
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[ContactPage]("listContacts", ListContactsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	path := harvestPagePath("/contacts", input.Cursor, input.PerPage)
	if input.ClientID > 0 {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		path += separator + "client_id=" + strconv.FormatInt(input.ClientID, 10)
	}
	return invokeGet(operation.client, call, "listContacts", path, ListContactsBranchListed, ListContactsBranchProviderRejected, decodeContactPage)
}

// GetContactOperation reads a single Harvest contact.
type GetContactOperation struct{ client *Client }

// GetContact binds the contact lookup Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated Query contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads a contact by ID.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactRecord] {
	if input.ID <= 0 {
		return failedBranch[ContactRecord]("getContact", GetContactBranchDefect, sdkgo.FailureValidation, "contact ID must be positive")
	}
	return invokeGet(operation.client, call, "getContact", "/contacts/"+strconv.FormatInt(input.ID, 10), GetContactBranchFound, GetContactBranchNotFound, decodeContactRecord)
}

// CreateContactOperation creates a contact with bounded repeat.
type CreateContactOperation struct{ client *Client }

// CreateContact binds the contact creation Mutation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// IdempotencyKey records the Dex call identity; Harvest does not deduplicate this POST.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a contact under a limited dispatch fence.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[ContactRecord] {
	if input.ClientID <= 0 || strings.TrimSpace(input.FirstName) == "" || strings.TrimSpace(input.LastName) == "" || len(input.FirstName) > 255 || len(input.LastName) > 255 || !validOptionalContactEmail(input.Email) {
		return harvestMutationFailure[ContactRecord]("createContact", CreateContactBranchDefect, sdkgo.FailureValidation, "contact client ID, name, or email is invalid")
	}
	payload, err := json.Marshal(struct {
		ClientID  int64  `json:"client_id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email,omitempty"`
	}{input.ClientID, input.FirstName, input.LastName, input.Email})
	if err != nil {
		return harvestMutationFailure[ContactRecord]("createContact", CreateContactBranchDefect, sdkgo.FailureValidation, "contact input cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createContact", CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, "/contacts", payload, CreateContactBranchCreated, CreateContactBranchProviderRejected, CreateContactBranchDefect, decodeContactRecord, func(contact ContactRecord) string { return strconv.FormatInt(contact.ID, 10) })
}

// UpdateContactOperation changes absolute contact fields.
type UpdateContactOperation struct{ client *Client }

// UpdateContact binds the contact update Mutation to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies an idempotent contact PATCH.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactRecord] {
	if input.ID <= 0 || (input.FirstName == nil && input.LastName == nil && input.Email == nil) ||
		(input.FirstName != nil && (strings.TrimSpace(*input.FirstName) == "" || len(*input.FirstName) > 255)) ||
		(input.LastName != nil && (strings.TrimSpace(*input.LastName) == "" || len(*input.LastName) > 255)) ||
		(input.Email != nil && !validOptionalContactEmail(*input.Email)) {
		return harvestMutationFailure[ContactRecord]("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "contact ID or fields are invalid")
	}
	payload, err := json.Marshal(struct {
		FirstName *string `json:"first_name,omitempty"`
		LastName  *string `json:"last_name,omitempty"`
		Email     *string `json:"email,omitempty"`
	}{input.FirstName, input.LastName, input.Email})
	if err != nil {
		return harvestMutationFailure[ContactRecord]("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "contact update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateContact", http.MethodPatch, "/contacts/"+strconv.FormatInt(input.ID, 10), payload, UpdateContactBranchUpdated, UpdateContactBranchNotFound, UpdateContactBranchProviderRejected, decodeContactRecord)
}

// DeleteContactOperation deletes one Harvest contact.
type DeleteContactOperation struct{ client *Client }

// DeleteContact binds the contact deletion Mutation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey records the stable Dex call identity.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends an idempotent contact DELETE.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeletedContact] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedContact]("deleteContact", DeleteContactBranchDefect, sdkgo.FailureValidation, "contact ID must be positive")
	}
	decode := func(_ []byte) (DeletedContact, error) { return DeletedContact{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteContact", http.MethodDelete, "/contacts/"+strconv.FormatInt(input.ID, 10), nil, DeleteContactBranchDeleted, DeleteContactBranchNotFound, DeleteContactBranchProviderRejected, decode)
}

func validOptionalContactEmail(email string) bool {
	if email == "" {
		return true
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && len(email) <= 320
}

func decodeContactRecord(body []byte) (ContactRecord, error) {
	var wire struct {
		ID     int64 `json:"id"`
		Client struct {
			ID int64 `json:"id"`
		} `json:"client"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactRecord{}, err
	}
	if wire.ID <= 0 || wire.Client.ID <= 0 || wire.FirstName == "" || wire.LastName == "" {
		return ContactRecord{}, fmt.Errorf("missing contact identity")
	}
	return ContactRecord{ID: wire.ID, ClientID: wire.Client.ID, FirstName: wire.FirstName, LastName: wire.LastName, Email: wire.Email}, nil
}

func decodeContactPage(body []byte) (ContactPage, error) {
	var wire struct {
		Contacts []json.RawMessage `json:"contacts"`
		Links    struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactPage{}, err
	}
	if wire.Contacts == nil {
		return ContactPage{}, fmt.Errorf("missing contacts")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/contacts")
	if err != nil {
		return ContactPage{}, err
	}
	page := ContactPage{Contacts: make([]ContactRecord, 0, len(wire.Contacts)), NextCursor: next}
	for _, raw := range wire.Contacts {
		contact, err := decodeContactRecord(raw)
		if err != nil {
			return ContactPage{}, err
		}
		page.Contacts = append(page.Contacts, contact)
	}
	return page, nil
}
