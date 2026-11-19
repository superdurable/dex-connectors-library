// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactListMembershipsInput selects one page of a contact's list memberships.
type ListContactListMembershipsInput struct {
	// ContactID is the numeric provider contact ID.
	ContactID string `json:"contactId"`
	// Offset is the zero-based number of memberships to skip.
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum membership count; zero uses 20 and 100 is the maximum.
	Limit int `json:"limit,omitempty"`
}

// SetContactListMembershipInput selects a contact and email list.
type SetContactListMembershipInput struct {
	// ContactID is the numeric provider contact ID.
	ContactID string `json:"contactId"`
	// ListID is the numeric provider email list ID.
	ListID string `json:"listId"`
}

// ContactListMembership contains one contact's subscription status on a list.
type ContactListMembership struct {
	// ID is the provider membership ID; empty when an absent membership already satisfies unsubscribe.
	ID string `json:"id"`
	// ContactID is the owning contact ID.
	ContactID string `json:"contactId"`
	// ListID is the subscribed list ID.
	ListID string `json:"listId"`
	// Status is 1 for active and 2 for unsubscribed; reads may also return 0 unconfirmed or 3 bounced.
	Status int `json:"status"`
}

// ContactListMembershipsPage is one page of a contact's list memberships.
type ContactListMembershipsPage struct {
	// Memberships contains the selected membership records.
	Memberships []ContactListMembership `json:"memberships"`
	// ContactID is the selected contact ID.
	ContactID string `json:"contactId"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
	// Total is the provider's matching membership count.
	Total int `json:"total"`
}

// ListContactListMembershipsOperation reads one page of a contact's list memberships.
type ListContactListMembershipsOperation struct{ client *Client }

// ListContactListMemberships binds the list Query to this client.
func (client *Client) ListContactListMemberships() ListContactListMembershipsOperation {
	return ListContactListMembershipsOperation{client}
}

// Definition returns the manifest-generated list contract.
func (ListContactListMembershipsOperation) Definition() sdkgo.QueryDefinition {
	return ListContactListMembershipsDefinition
}

// Invoke reads one page of list membership statuses for a contact.
func (operation ListContactListMembershipsOperation) Invoke(call sdkgo.Call, input ListContactListMembershipsInput) sdkgo.QueryAttempt[ContactListMembershipsPage] {
	branches := queryBranches{found: ListContactListMembershipsBranchListed, rejected: ListContactListMembershipsBranchProviderRejected, invalid: ListContactListMembershipsBranchInvalidResponse, defect: ListContactListMembershipsBranchDefect}
	if !numericID.MatchString(input.ContactID) || input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listContactListMemberships", branches.defect, ContactListMembershipsPage{}, sdkgo.FailureValidation, "contact ID, offset, or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"contact": {input.ContactID}, "limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return invokeQuery(call, operation.client, "listContactListMemberships", "/contactLists?"+query.Encode(), branches, func(body []byte) (ContactListMembershipsPage, error) {
		return decodeContactListMemberships(body, input)
	})
}

type contactListWire struct {
	ID      string          `json:"id"`
	Contact string          `json:"contact"`
	List    string          `json:"list"`
	Status  json.RawMessage `json:"status"`
}

func decodeContactListMemberships(body []byte, input ListContactListMembershipsInput) (ContactListMembershipsPage, error) {
	var wire struct {
		ContactLists []contactListWire `json:"contactLists"`
		Meta         struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactListMembershipsPage{}, err
	}
	if wire.ContactLists == nil {
		return ContactListMembershipsPage{}, fmt.Errorf("membership collection missing")
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total < 0 {
		return ContactListMembershipsPage{}, fmt.Errorf("membership count missing or invalid")
	}
	page := ContactListMembershipsPage{Memberships: make([]ContactListMembership, 0, len(wire.ContactLists)), ContactID: input.ContactID, Offset: input.Offset, Limit: input.Limit, Total: total}
	for _, item := range wire.ContactLists {
		membership, err := contactListMembership(item)
		if err != nil || membership.ContactID != input.ContactID {
			return ContactListMembershipsPage{}, fmt.Errorf("membership identity invalid")
		}
		page.Memberships = append(page.Memberships, membership)
	}
	return page, nil
}

func decodeContactListMembership(body []byte) (ContactListMembership, error) {
	var wire struct {
		ContactList *contactListWire `json:"contactList"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactListMembership{}, err
	}
	if wire.ContactList == nil {
		return ContactListMembership{}, fmt.Errorf("membership missing")
	}
	return contactListMembership(*wire.ContactList)
}

func contactListMembership(item contactListWire) (ContactListMembership, error) {
	if !numericID.MatchString(item.ID) || !numericID.MatchString(item.Contact) || !numericID.MatchString(item.List) {
		return ContactListMembership{}, fmt.Errorf("membership identity missing")
	}
	status, err := strconv.Atoi(strings.Trim(string(item.Status), `"`))
	if err != nil || status < 0 || status > 3 {
		return ContactListMembership{}, fmt.Errorf("membership status invalid")
	}
	return ContactListMembership{ID: item.ID, ContactID: item.Contact, ListID: item.List, Status: status}, nil
}
