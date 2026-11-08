// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactViewsInput selects the visible Freshsales contact views.
type ListContactViewsInput struct{}

// ContactViewMetadata identifies one Freshsales contact view.
type ContactViewMetadata struct {
	// ID is the stable view identifier used by the contacts view API.
	ID int64 `json:"id"`
	// Name is the label shown in Freshsales.
	Name string `json:"name"`
}

// ContactViews contains the contact views visible to the API user.
type ContactViews struct {
	// Views contains IDs and names without filter internals.
	Views []ContactViewMetadata `json:"views"`
}

// GetContactInput selects one Freshsales contact by numeric ID.
type GetContactInput struct {
	// ContactID is the decimal ID shown by Freshsales and its API.
	ContactID string `json:"contactId"`
}

// ContactMetadata contains contact identity without custom fields or links.
type ContactMetadata struct {
	// ID is the stable Freshsales contact identifier.
	ID int64 `json:"id"`
	// FirstName is the contact's given name when available.
	FirstName string `json:"firstName"`
	// LastName is the contact's family name when available.
	LastName string `json:"lastName"`
	// EmailAddress is the contact's email address when available.
	EmailAddress string `json:"emailAddress"`
}

// ListContactViewsOperation reads available contact views.
type ListContactViewsOperation struct{ client *Client }

// GetContactOperation reads one contact from Freshsales.
type GetContactOperation struct{ client *Client }

// ListContactViews binds view discovery to this client.
func (client *Client) ListContactViews() ListContactViewsOperation {
	return ListContactViewsOperation{client}
}

// GetContact binds the contact Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated contact view Query contract.
func (ListContactViewsOperation) Definition() sdkgo.QueryDefinition {
	return ListContactViewsDefinition
}

// Definition returns the generated contact Query contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads the contact view IDs and names visible to the API user.
func (operation ListContactViewsOperation) Invoke(call sdkgo.Call, _ ListContactViewsInput) sdkgo.QueryAttempt[ContactViews] {
	branches := queryBranches{found: ListContactViewsBranchListed, rejected: ListContactViewsBranchProviderRejected, invalid: ListContactViewsBranchInvalidResponse, defect: ListContactViewsBranchDefect}
	return invokeQuery(call, operation.client, "listContactViews", "/contacts/filters", branches, decodeContactViews)
}

// Invoke reads one contact's basic identity by ID.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactMetadata] {
	branches := queryBranches{found: GetContactBranchFound, notFound: GetContactBranchNotFound, rejected: GetContactBranchProviderRejected, invalid: GetContactBranchInvalidResponse, defect: GetContactBranchDefect}
	if !isPositiveContactID(input.ContactID) {
		return queryBranch("getContact", branches.defect, ContactMetadata{}, sdkgo.FailureValidation, "contact ID must be a positive decimal identifier")
	}
	return invokeQuery(call, operation.client, "getContact", "/contacts/"+input.ContactID, branches, decodeContact)
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

func decodeContactViews(body []byte) (ContactViews, error) {
	var wire struct {
		Filters []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"filters"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactViews{}, err
	}
	if wire.Filters == nil {
		return ContactViews{}, fmt.Errorf("contact views missing")
	}
	views := make([]ContactViewMetadata, 0, len(wire.Filters))
	for _, view := range wire.Filters {
		if view.ID <= 0 || view.Name == "" {
			return ContactViews{}, fmt.Errorf("contact view identity missing")
		}
		views = append(views, ContactViewMetadata{ID: view.ID, Name: view.Name})
	}
	return ContactViews{Views: views}, nil
}

func decodeContact(body []byte) (ContactMetadata, error) {
	var wire struct {
		Contact *struct {
			ID           int64  `json:"id"`
			FirstName    string `json:"first_name"`
			LastName     string `json:"last_name"`
			EmailAddress string `json:"email"`
		} `json:"contact"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactMetadata{}, err
	}
	if wire.Contact == nil || wire.Contact.ID <= 0 {
		return ContactMetadata{}, fmt.Errorf("contact identity missing")
	}
	return ContactMetadata{ID: wire.Contact.ID, FirstName: wire.Contact.FirstName, LastName: wire.Contact.LastName, EmailAddress: wire.Contact.EmailAddress}, nil
}
