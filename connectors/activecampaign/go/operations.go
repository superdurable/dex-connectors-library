// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactsInput selects an offset-based page of at most 100 contacts.
type ListContactsInput struct {
	// Offset is the zero-based number of contacts to skip.
	Offset int `json:"offset"`
	// Limit is the maximum contact count; zero defaults to 20.
	Limit int `json:"limit,omitempty"`
}

// ContactMetadata contains contact identity and name without embedded relationships.
type ContactMetadata struct {
	// ID is the stable provider contact ID.
	ID string `json:"id"`
	// Email is the contact email address, if present.
	Email string `json:"email"`
	// FirstName is the contact given name, if present.
	FirstName string `json:"firstName"`
	// LastName is the contact family name, if present.
	LastName string `json:"lastName"`
}

// ContactsPage is one offset-based contact page.
type ContactsPage struct {
	// Contacts contains the selected contact records.
	Contacts []ContactMetadata `json:"contacts"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
	// Total is the provider's matching contact count.
	Total int `json:"total"`
}

// GetContactInput selects one numeric ActiveCampaign contact ID.
type GetContactInput struct {
	// ContactID is the numeric ID from the Contacts page or API.
	ContactID string `json:"contactId"`
}

// ListContactsOperation retrieves one page of contact metadata.
type ListContactsOperation struct{ client *Client }

// GetContactOperation retrieves one contact's metadata.
type GetContactOperation struct{ client *Client }

// ListContacts binds the list Query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// GetContact binds the single-contact Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Definition returns the manifest-generated contact contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads one page of contact identities and names.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	branches := queryBranches{found: ListContactsBranchListed, rejected: ListContactsBranchProviderRejected, invalid: ListContactsBranchInvalidResponse, defect: ListContactsBranchDefect}
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listContacts", branches.defect, ContactsPage{}, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return invokeQuery(call, operation.client, "listContacts", "/contacts?"+query.Encode(), branches, func(body []byte) (ContactsPage, error) { return decodeContactsPage(body, input) })
}

// Invoke reads one contact without embedded relationship records.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactMetadata] {
	branches := queryBranches{found: GetContactBranchFound, notFound: GetContactBranchNotFound, rejected: GetContactBranchProviderRejected, invalid: GetContactBranchInvalidResponse, defect: GetContactBranchDefect}
	if !numericID.MatchString(input.ContactID) {
		return queryBranch("getContact", branches.defect, ContactMetadata{}, sdkgo.FailureValidation, "contact ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getContact", "/contacts/"+input.ContactID, branches, decodeContact)
}

var numericID = regexp.MustCompile(`^[0-9]{1,24}$`)

type contactWire struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

func decodeContactsPage(body []byte, input ListContactsInput) (ContactsPage, error) {
	var wire struct {
		Contacts []contactWire `json:"contacts"`
		Meta     struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactsPage{}, err
	}
	if wire.Contacts == nil {
		return ContactsPage{}, fmt.Errorf("contact list missing")
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total < 0 {
		return ContactsPage{}, fmt.Errorf("contact count missing or invalid")
	}
	page := ContactsPage{Contacts: make([]ContactMetadata, 0, len(wire.Contacts)), Offset: input.Offset, Limit: input.Limit, Total: total}
	for _, contact := range wire.Contacts {
		metadata, err := contactMetadata(contact)
		if err != nil {
			return ContactsPage{}, err
		}
		page.Contacts = append(page.Contacts, metadata)
	}
	return page, nil
}

func decodeContact(body []byte) (ContactMetadata, error) {
	var wire struct {
		Contact *contactWire `json:"contact"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactMetadata{}, err
	}
	if wire.Contact == nil {
		return ContactMetadata{}, fmt.Errorf("contact missing")
	}
	return contactMetadata(*wire.Contact)
}

func contactMetadata(contact contactWire) (ContactMetadata, error) {
	if !numericID.MatchString(contact.ID) {
		return ContactMetadata{}, fmt.Errorf("contact ID missing or invalid")
	}
	return ContactMetadata{ID: contact.ID, Email: contact.Email, FirstName: contact.FirstName, LastName: contact.LastName}, nil
}
