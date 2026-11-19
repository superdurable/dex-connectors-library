// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListAccountContactsInput selects one page of account-contact links.
type ListAccountContactsInput struct {
	// AccountID optionally filters by numeric CRM account ID.
	AccountID string `json:"accountId,omitempty"`
	// ContactID optionally filters by numeric contact ID.
	ContactID string `json:"contactId,omitempty"`
	// Offset is the zero-based number of links to skip.
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum link count; zero uses 20 and 100 is the maximum.
	Limit int `json:"limit,omitempty"`
}

// GetAccountContactInput selects one link by numeric provider ID.
type GetAccountContactInput struct {
	// AssociationID is the numeric account-contact link ID.
	AssociationID string `json:"associationId"`
}

// AccountContactAssociation identifies a contact's CRM account link.
type AccountContactAssociation struct {
	// ID is the numeric provider link ID.
	ID string `json:"id"`
	// AccountID is the linked CRM account.
	AccountID string `json:"accountId"`
	// ContactID is the linked contact.
	ContactID string `json:"contactId"`
	// JobTitle is the contact's optional role at the account.
	JobTitle string `json:"jobTitle"`
}

// AccountContactsPage contains one offset-based page of account-contact links.
type AccountContactsPage struct {
	// Associations contains the selected links.
	Associations []AccountContactAssociation `json:"associations"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
	// Total is the provider's matching link count.
	Total int `json:"total"`
}

// ListAccountContactsOperation reads one page of links.
type ListAccountContactsOperation struct{ client *Client }

// GetAccountContactOperation reads one link.
type GetAccountContactOperation struct{ client *Client }

// ListAccountContacts binds the account-contact listing Query.
func (client *Client) ListAccountContacts() ListAccountContactsOperation {
	return ListAccountContactsOperation{client}
}

// GetAccountContact binds the account-contact lookup Query.
func (client *Client) GetAccountContact() GetAccountContactOperation {
	return GetAccountContactOperation{client}
}

// Definition returns the manifest-generated listing contract.
func (ListAccountContactsOperation) Definition() sdkgo.QueryDefinition {
	return ListAccountContactsDefinition
}

// Definition returns the manifest-generated lookup contract.
func (GetAccountContactOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountContactDefinition
}

// Invoke reads filtered account-contact links.
func (operation ListAccountContactsOperation) Invoke(call sdkgo.Call, input ListAccountContactsInput) sdkgo.QueryAttempt[AccountContactsPage] {
	branches := queryBranches{found: ListAccountContactsBranchListed, rejected: ListAccountContactsBranchProviderRejected, invalid: ListAccountContactsBranchInvalidResponse, defect: ListAccountContactsBranchDefect}
	if input.AccountID != "" && !numericID.MatchString(input.AccountID) || input.ContactID != "" && !numericID.MatchString(input.ContactID) || input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listAccountContacts", branches.defect, AccountContactsPage{}, sdkgo.FailureValidation, "account, contact, offset, or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	if input.AccountID != "" {
		query.Set("filters[account]", input.AccountID)
	}
	if input.ContactID != "" {
		query.Set("filters[contact]", input.ContactID)
	}
	return invokeQuery(call, operation.client, "listAccountContacts", "/accountContacts?"+query.Encode(), branches, func(body []byte) (AccountContactsPage, error) {
		return decodeAccountContactsPage(body, input)
	})
}

// Invoke reads one account-contact link.
func (operation GetAccountContactOperation) Invoke(call sdkgo.Call, input GetAccountContactInput) sdkgo.QueryAttempt[AccountContactAssociation] {
	branches := queryBranches{found: GetAccountContactBranchFound, notFound: GetAccountContactBranchNotFound, rejected: GetAccountContactBranchProviderRejected, invalid: GetAccountContactBranchInvalidResponse, defect: GetAccountContactBranchDefect}
	if !numericID.MatchString(input.AssociationID) {
		return queryBranch("getAccountContact", branches.defect, AccountContactAssociation{}, sdkgo.FailureValidation, "association ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getAccountContact", "/accountContacts/"+input.AssociationID, branches, decodeAccountContact)
}

type accountContactWire struct {
	ID       string `json:"id"`
	Account  string `json:"account"`
	Contact  string `json:"contact"`
	JobTitle string `json:"jobTitle"`
}

func decodeAccountContactsPage(body []byte, input ListAccountContactsInput) (AccountContactsPage, error) {
	var wire struct {
		AccountContacts []accountContactWire `json:"accountContacts"`
		Meta            struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AccountContactsPage{}, err
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if wire.AccountContacts == nil || err != nil || total < 0 {
		return AccountContactsPage{}, fmt.Errorf("link collection or count missing")
	}
	page := AccountContactsPage{Associations: make([]AccountContactAssociation, 0, len(wire.AccountContacts)), Offset: input.Offset, Limit: input.Limit, Total: total}
	for _, item := range wire.AccountContacts {
		link, err := accountContactAssociation(item)
		if err != nil {
			return AccountContactsPage{}, err
		}
		page.Associations = append(page.Associations, link)
	}
	return page, nil
}

func decodeAccountContact(body []byte) (AccountContactAssociation, error) {
	var wire struct {
		AccountContact *accountContactWire `json:"accountContact"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AccountContactAssociation{}, err
	}
	if wire.AccountContact == nil {
		return AccountContactAssociation{}, fmt.Errorf("account-contact link missing")
	}
	return accountContactAssociation(*wire.AccountContact)
}

func accountContactAssociation(wire accountContactWire) (AccountContactAssociation, error) {
	if !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.Account) || !numericID.MatchString(wire.Contact) {
		return AccountContactAssociation{}, fmt.Errorf("account-contact link identity invalid")
	}
	return AccountContactAssociation{ID: wire.ID, AccountID: wire.Account, ContactID: wire.Contact, JobTitle: wire.JobTitle}, nil
}
