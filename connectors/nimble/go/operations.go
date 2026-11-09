// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package nimble

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactIDsInput selects one Nimble contact ID page.
type ListContactIDsInput struct {
	// Page is the one-based page number.
	Page int `json:"page"`
	// PerPage is the bounded page size, from 1 through 100.
	PerPage int `json:"perPage"`
}

// ContactIDsPage contains one page of stable contact IDs and pagination metadata.
type ContactIDsPage struct {
	// IDs are the contact identifiers returned by Nimble.
	IDs []string `json:"ids"`
	// Page is the returned one-based page number.
	Page int `json:"page"`
	// Pages is the total number of available pages.
	Pages int `json:"pages"`
	// PerPage is the provider's page size.
	PerPage int `json:"perPage"`
	// Total is the total number of matching contacts.
	Total int `json:"total"`
}

// GetContactInput selects one Nimble contact by its BSON identifier.
type GetContactInput struct {
	// ContactID is the 24-character hexadecimal ID returned by Nimble.
	ContactID string `json:"contactId"`
}

// ContactMetadata contains selected contact identity without custom fields or links.
type ContactMetadata struct {
	// ID is the stable Nimble contact identifier.
	ID string `json:"id"`
	// FirstName is the first name when present.
	FirstName string `json:"firstName"`
	// LastName is the last name when present.
	LastName string `json:"lastName"`
	// EmailAddress is the first email value when present.
	EmailAddress string `json:"emailAddress"`
}

// ListContactIDsOperation reads a contact ID page from Nimble.
type ListContactIDsOperation struct{ client *Client }

// GetContactOperation reads one contact from Nimble.
type GetContactOperation struct{ client *Client }

// ListContactIDs binds contact ID discovery to this client.
func (client *Client) ListContactIDs() ListContactIDsOperation {
	return ListContactIDsOperation{client}
}

// GetContact binds the contact Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated contact ID page Query contract.
func (ListContactIDsOperation) Definition() sdkgo.QueryDefinition { return ListContactIDsDefinition }

// Definition returns the generated contact Query contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads one bounded page of contact IDs.
func (operation ListContactIDsOperation) Invoke(call sdkgo.Call, input ListContactIDsInput) sdkgo.QueryAttempt[ContactIDsPage] {
	branches := queryBranches{found: ListContactIDsBranchListed, rejected: ListContactIDsBranchProviderRejected, invalid: ListContactIDsBranchInvalidResponse, defect: ListContactIDsBranchDefect}
	if input.Page < 1 || input.PerPage < 1 || input.PerPage > 100 {
		return queryBranch("listContactIDs", branches.defect, ContactIDsPage{}, sdkgo.FailureValidation, "contact page number or size is invalid")
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per_page": {strconv.Itoa(input.PerPage)}}
	return invokeQuery(call, operation.client, "listContactIDs", "/contacts/ids?"+query.Encode(), branches, decodeContactIDsPage)
}

// Invoke reads one contact by ID and excludes custom fields.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactMetadata] {
	branches := queryBranches{found: GetContactBranchFound, notFound: GetContactBranchNotFound, rejected: GetContactBranchProviderRejected, invalid: GetContactBranchInvalidResponse, defect: GetContactBranchDefect}
	if !isContactID(input.ContactID) {
		return queryBranch("getContact", branches.defect, ContactMetadata{}, sdkgo.FailureValidation, "contact ID must be a 24-character hexadecimal identifier")
	}
	return invokeQuery(call, operation.client, "getContact", "/contact/"+input.ContactID, branches, func(body []byte) (ContactMetadata, error) { return decodeContact(body, input.ContactID) })
}

func isContactID(contactID string) bool {
	if len(contactID) != 24 {
		return false
	}
	for _, digit := range contactID {
		if !((digit >= '0' && digit <= '9') || (digit >= 'a' && digit <= 'f') || (digit >= 'A' && digit <= 'F')) {
			return false
		}
	}
	return true
}

func decodeContactIDsPage(body []byte) (ContactIDsPage, error) {
	var wire struct {
		Meta struct {
			Page    int `json:"page"`
			Pages   int `json:"pages"`
			PerPage int `json:"per_page"`
			Total   int `json:"total"`
		} `json:"meta"`
		Resources []string `json:"resources"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactIDsPage{}, err
	}
	if wire.Resources == nil || wire.Meta.Page < 1 || wire.Meta.Pages < 0 || wire.Meta.PerPage < 1 || wire.Meta.Total < 0 {
		return ContactIDsPage{}, fmt.Errorf("contact ID page metadata missing")
	}
	for _, contactID := range wire.Resources {
		if !isContactID(contactID) {
			return ContactIDsPage{}, fmt.Errorf("contact identifier invalid")
		}
	}
	return ContactIDsPage{IDs: wire.Resources, Page: wire.Meta.Page, Pages: wire.Meta.Pages, PerPage: wire.Meta.PerPage, Total: wire.Meta.Total}, nil
}

func decodeContact(body []byte, requestedID string) (ContactMetadata, error) {
	var wire struct {
		Resources []struct {
			ID     string `json:"id"`
			Fields map[string][]struct {
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactMetadata{}, err
	}
	if len(wire.Resources) != 1 || !isContactID(wire.Resources[0].ID) || !strings.EqualFold(wire.Resources[0].ID, requestedID) {
		return ContactMetadata{}, fmt.Errorf("contact identity missing or mismatched")
	}
	contact := wire.Resources[0]
	return ContactMetadata{ID: contact.ID, FirstName: firstFieldValue(contact.Fields["first name"]), LastName: firstFieldValue(contact.Fields["last name"]), EmailAddress: firstFieldValue(contact.Fields["email"])}, nil
}

func firstFieldValue(values []struct {
	Value string `json:"value"`
}) string {
	if len(values) == 0 {
		return ""
	}
	return values[0].Value
}
