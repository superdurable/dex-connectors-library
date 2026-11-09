// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getresponse

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactsInput selects a one-based page of at most 1000 contacts.
type ListContactsInput struct {
	// Page is the one-based page number; zero defaults to one.
	Page int `json:"page,omitempty"`
	// PerPage is the maximum count; zero defaults to 20.
	PerPage int `json:"perPage,omitempty"`
}

// ContactMetadata contains contact identity and name without embedded fields.
type ContactMetadata struct {
	// ContactID is the stable GetResponse contact ID.
	ContactID string `json:"contactId"`
	// Email is the contact email address.
	Email string `json:"email"`
	// Name is the contact name, when present.
	Name string `json:"name"`
}

// ContactsPage contains one page of contact metadata.
type ContactsPage struct {
	// Contacts contains the returned contact records.
	Contacts []ContactMetadata `json:"contacts"`
	// Page is the requested one-based page.
	Page int `json:"page"`
	// PerPage is the requested maximum count.
	PerPage int `json:"perPage"`
}

// GetContactInput selects one GetResponse contact ID.
type GetContactInput struct {
	// ContactID is the ID from a contact page or API.
	ContactID string `json:"contactId"`
}

// ListContactsOperation retrieves one page of contact metadata.
type ListContactsOperation struct{ client *Client }

// GetContactOperation retrieves one contact's metadata.
type GetContactOperation struct{ client *Client }

// ListContacts binds the list Query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// GetContact binds the contact Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Definition returns the manifest-generated contact contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads one page of contact identities and names.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	branches := queryBranches{found: ListContactsBranchListed, rejected: ListContactsBranchProviderRejected, invalid: ListContactsBranchInvalidResponse, defect: ListContactsBranchDefect}
	if input.Page < 0 || input.PerPage < 0 || input.PerPage > 1000 {
		return queryBranch("listContacts", branches.defect, ContactsPage{}, sdkgo.FailureValidation, "page or perPage is invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PerPage == 0 {
		input.PerPage = 20
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "perPage": {strconv.Itoa(input.PerPage)}}
	return invokeQuery(call, operation.client, "listContacts", "/contacts?"+query.Encode(), branches, func(body []byte) (ContactsPage, error) { return decodeContactsPage(body, input) })
}

// Invoke reads one contact without embedded fields.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactMetadata] {
	branches := queryBranches{found: GetContactBranchFound, notFound: GetContactBranchNotFound, rejected: GetContactBranchProviderRejected, invalid: GetContactBranchInvalidResponse, defect: GetContactBranchDefect}
	if !contactIDPattern.MatchString(input.ContactID) {
		return queryBranch("getContact", branches.defect, ContactMetadata{}, sdkgo.FailureValidation, "contact ID must be alphanumeric")
	}
	return invokeQuery(call, operation.client, "getContact", "/contacts/"+input.ContactID, branches, decodeContact)
}

var contactIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

type contactWire struct {
	ContactID string  `json:"contactId"`
	Email     string  `json:"email"`
	Name      *string `json:"name"`
}

func decodeContactsPage(body []byte, input ListContactsInput) (ContactsPage, error) {
	var wire []contactWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactsPage{}, err
	}
	if wire == nil {
		return ContactsPage{}, fmt.Errorf("contact list missing")
	}
	page := ContactsPage{Contacts: make([]ContactMetadata, 0, len(wire)), Page: input.Page, PerPage: input.PerPage}
	for _, contact := range wire {
		metadata, err := contactMetadata(contact)
		if err != nil {
			return ContactsPage{}, err
		}
		page.Contacts = append(page.Contacts, metadata)
	}
	return page, nil
}

func decodeContact(body []byte) (ContactMetadata, error) {
	var wire contactWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactMetadata{}, err
	}
	return contactMetadata(wire)
}

func contactMetadata(contact contactWire) (ContactMetadata, error) {
	if !contactIDPattern.MatchString(contact.ContactID) || contact.Email == "" {
		return ContactMetadata{}, fmt.Errorf("contact identity missing or invalid")
	}
	name := ""
	if contact.Name != nil {
		name = *contact.Name
	}
	return ContactMetadata{ContactID: contact.ContactID, Email: contact.Email, Name: name}, nil
}
