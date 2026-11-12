// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package insightly

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactsInput selects a bounded Insightly contact page.
type ListContactsInput struct {
	// Top is the maximum page size, from 1 to 500 records.
	Top int `json:"top"`
	// Skip is the nonnegative number of contacts to omit before this page; zero starts at the first contact.
	Skip int `json:"skip,omitempty"`
}

// ContactMetadata contains public contact identity without custom fields or links.
type ContactMetadata struct {
	// ID is the stable Insightly contact identifier.
	ID int64 `json:"id"`
	// FirstName is the contact's given name when available.
	FirstName string `json:"firstName"`
	// LastName is the contact's family name when available.
	LastName string `json:"lastName"`
	// EmailAddress is the contact's primary email address when available.
	EmailAddress string `json:"emailAddress"`
}

// ContactsPage contains the requested contact page.
type ContactsPage struct {
	// Contacts contains only contact identity fields.
	Contacts []ContactMetadata `json:"contacts"`
}

// GetContactInput selects one Insightly contact by its numeric ID.
type GetContactInput struct {
	// ContactID is the decimal ID shown in Insightly and returned by its API.
	ContactID string `json:"contactId"`
}

// ListContactsOperation reads a contact page from Insightly.
type ListContactsOperation struct{ client *Client }

// GetContactOperation reads one contact from Insightly.
type GetContactOperation struct{ client *Client }

// ListContacts binds contact page discovery to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// GetContact binds the contact Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated contact page Query contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Definition returns the generated contact Query contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads one contact page, excluding custom fields and links.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	branches := queryBranches{found: ListContactsBranchListed, rejected: ListContactsBranchProviderRejected, invalid: ListContactsBranchInvalidResponse, defect: ListContactsBranchDefect}
	if input.Top < 1 || input.Top > 500 || input.Skip < 0 {
		return queryBranch("listContacts", branches.defect, ContactsPage{}, sdkgo.FailureValidation, "contact page size or offset is invalid")
	}
	query := url.Values{"top": {strconv.Itoa(input.Top)}, "skip": {strconv.Itoa(input.Skip)}}
	return invokeQuery(call, operation.client, "listContacts", "/Contacts?"+query.Encode(), branches, decodeContactsPage)
}

// Invoke reads one contact by ID, excluding custom fields and links.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactMetadata] {
	branches := queryBranches{found: GetContactBranchFound, notFound: GetContactBranchNotFound, rejected: GetContactBranchProviderRejected, invalid: GetContactBranchInvalidResponse, defect: GetContactBranchDefect}
	if !isPositiveContactID(input.ContactID) {
		return queryBranch("getContact", branches.defect, ContactMetadata{}, sdkgo.FailureValidation, "contact ID must be a positive decimal identifier")
	}
	return invokeQuery(call, operation.client, "getContact", "/Contacts/"+input.ContactID, branches, decodeContact)
}

func isPositiveContactID(contactID string) bool {
	if contactID == "" {
		return false
	}
	for _, digit := range contactID {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	value, err := strconv.ParseUint(contactID, 10, 63)
	return err == nil && value > 0
}

func decodeContactsPage(body []byte) (ContactsPage, error) {
	var wire []struct {
		ID           int64  `json:"CONTACT_ID"`
		FirstName    string `json:"FIRST_NAME"`
		LastName     string `json:"LAST_NAME"`
		EmailAddress string `json:"EMAIL_ADDRESS"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactsPage{}, err
	}
	if wire == nil {
		return ContactsPage{}, fmt.Errorf("contact page missing")
	}
	contacts := make([]ContactMetadata, 0, len(wire))
	for _, contact := range wire {
		if contact.ID <= 0 {
			return ContactsPage{}, fmt.Errorf("contact identity missing")
		}
		contacts = append(contacts, ContactMetadata{ID: contact.ID, FirstName: contact.FirstName, LastName: contact.LastName, EmailAddress: contact.EmailAddress})
	}
	return ContactsPage{Contacts: contacts}, nil
}

func decodeContact(body []byte) (ContactMetadata, error) {
	var wire struct {
		ID           int64  `json:"CONTACT_ID"`
		FirstName    string `json:"FIRST_NAME"`
		LastName     string `json:"LAST_NAME"`
		EmailAddress string `json:"EMAIL_ADDRESS"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactMetadata{}, err
	}
	if wire.ID <= 0 {
		return ContactMetadata{}, fmt.Errorf("contact identity missing")
	}
	return ContactMetadata{ID: wire.ID, FirstName: wire.FirstName, LastName: wire.LastName, EmailAddress: wire.EmailAddress}, nil
}
