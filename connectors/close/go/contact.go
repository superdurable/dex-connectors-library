// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Contact identifies a person and the lead that owns them.
type Contact struct {
	// ID is Close's contact identifier.
	ID string `json:"id"`
	// Name is the contact's name or display name.
	Name string `json:"name"`
	// LeadID is the parent lead identifier.
	LeadID string `json:"leadId"`
}

// ContactPage contains a bounded contact page.
type ContactPage struct {
	// Contacts holds the returned contacts.
	Contacts []Contact `json:"contacts"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListContactsInput selects a page under one lead.
type ListContactsInput struct {
	// LeadID is the parent lead identifier.
	LeadID string `json:"leadId"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
	// Limit caps returned contacts; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// GetContactInput identifies one Close contact.
type GetContactInput struct {
	// ContactID is the Close cont_ identifier.
	ContactID string `json:"contactId"`
}

// UpdateContactInput replaces selected contact fields.
type UpdateContactInput struct {
	// ContactID identifies the contact to change.
	ContactID string `json:"contactId"`
	// Name replaces the contact name when present.
	Name *string `json:"name,omitempty"`
	// Title replaces the contact title when present; empty clears it.
	Title *string `json:"title,omitempty"`
}

// DeleteContactInput identifies one contact to remove.
type DeleteContactInput struct {
	// ContactID is the Close cont_ identifier.
	ContactID string `json:"contactId"`
}

// DeletedContact confirms that a contact is absent.
type DeletedContact struct {
	// ContactID identifies the absent contact.
	ContactID string `json:"contactId"`
}

// ListContactsOperation reads contacts under a lead.
type ListContactsOperation struct{ client *Client }

// ListContacts binds the Query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// Definition returns the generated listContacts contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Invoke validates pagination and classifies the Close response.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactPage] {
	if !validLeadID(input.LeadID) || input.Skip < 0 || input.Limit < 0 || input.Limit > 100 {
		return failedBranch[ContactPage]("listContacts", ListContactsBranchDefect, sdkgo.FailureValidation, "leadId, skip, or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"lead_id": {input.LeadID}, "_skip": {strconv.Itoa(input.Skip)}, "_limit": {strconv.Itoa(limit)}}
	return invokeGet(operation.client, call, "listContacts", "/contact/?"+query.Encode(), ListContactsBranchListed, ListContactsBranchProviderRejected, decodeContactPage)
}

// GetContactOperation reads one contact.
type GetContactOperation struct{ client *Client }

// GetContact binds the Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated getContact contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke validates identity and classifies the Close response.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[Contact] {
	if !validContactID(input.ContactID) {
		return failedBranch[Contact]("getContact", GetContactBranchDefect, sdkgo.FailureValidation, "contactId must be a Close contact ID")
	}
	return invokeGet(operation.client, call, "getContact", "/contact/"+input.ContactID+"/", GetContactBranchFound, GetContactBranchNotFound, decodeContact)
}

// UpdateContactOperation replaces selected contact fields.
type UpdateContactOperation struct{ client *Client }

// UpdateContact binds the Mutation to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// Definition returns the generated updateContact contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// IdempotencyKey correlates attempts; absolute field replacement is repeat safe.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates the replacement and classifies the Close response.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[Contact] {
	if !validContactID(input.ContactID) || (input.Name == nil && input.Title == nil) ||
		(input.Name != nil && (strings.TrimSpace(*input.Name) == "" || len(*input.Name) > 1000)) ||
		(input.Title != nil && len(*input.Title) > 1000) {
		return closeMutationBranch[Contact]("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "contactId and at least one valid replacement field are required")
	}
	requestBody, err := json.Marshal(struct {
		Name  *string `json:"name,omitempty"`
		Title *string `json:"title,omitempty"`
	}{Name: input.Name, Title: input.Title})
	if err != nil {
		return closeMutationBranch[Contact]("updateContact", UpdateContactBranchDefect, sdkgo.FailureLocalDefect, "contact update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateContact", http.MethodPut, "/contact/"+input.ContactID+"/", requestBody, UpdateContactBranchUpdated, UpdateContactBranchNotFound, decodeContact)
}

// DeleteContactOperation removes one contact.
type DeleteContactOperation struct{ client *Client }

// DeleteContact binds the Mutation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the generated deleteContact contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey correlates attempts; removing an absent contact is repeat safe.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates identity and classifies absence as a completed deletion.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeletedContact] {
	if !validContactID(input.ContactID) {
		return closeMutationBranch[DeletedContact]("deleteContact", DeleteContactBranchDefect, sdkgo.FailureValidation, "contactId must be a Close contact ID")
	}
	return invokeCloseWrite(operation.client, call, "deleteContact", http.MethodDelete, "/contact/"+input.ContactID+"/", nil, DeleteContactBranchDeleted, DeleteContactBranchDeleted, func(body []byte) (DeletedContact, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedContact{}, errors.New("invalid deletion response")
		}
		return DeletedContact{ContactID: input.ContactID}, nil
	})
}

func validContactID(contactID string) bool {
	return strings.HasPrefix(contactID, "cont_") && safeKey.MatchString(contactID)
}

func decodeContact(body []byte) (Contact, error) {
	var wire struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		LeadID      string `json:"lead_id"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Contact{}, err
	}
	if !validContactID(wire.ID) || !validLeadID(wire.LeadID) {
		return Contact{}, fmt.Errorf("incomplete contact")
	}
	name := wire.Name
	if name == "" {
		name = wire.DisplayName
	}
	return Contact{ID: wire.ID, Name: name, LeadID: wire.LeadID}, nil
}

func decodeContactPage(body []byte) (ContactPage, error) {
	var wire struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactPage{}, err
	}
	if wire.Data == nil {
		return ContactPage{}, errors.New("contact page is missing")
	}
	page := ContactPage{Contacts: make([]Contact, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, row := range wire.Data {
		contact, err := decodeContact(row)
		if err != nil {
			return ContactPage{}, err
		}
		page.Contacts = append(page.Contacts, contact)
	}
	return page, nil
}
