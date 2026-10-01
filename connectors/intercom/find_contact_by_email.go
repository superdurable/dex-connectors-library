// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultContactLimit is the number of contacts findContactByEmail returns when ContactLimit is zero.
	DefaultContactLimit = 10
	// MaxContactLimit is the largest accepted ContactLimit.
	MaxContactLimit = 50

	findContactByEmailOperation = "findContactByEmail"
)

// FindContactByEmailInput names one email address to look up.
type FindContactByEmailInput struct {
	// Email is one bare email address, such as jane@example.com, matched with Intercom's = operator.
	Email string `json:"email"`
	// ContactLimit is 1 to MaxContactLimit contacts; zero uses DefaultContactLimit.
	ContactLimit int `json:"contactLimit,omitempty"`
}

// Contact is the connector's view of one Intercom user or lead. Custom attributes, location, device,
// and engagement fields are omitted.
type Contact struct {
	// ID is the Intercom contact ID.
	ID string `json:"id"`
	// Role is Intercom's contact role: user or lead.
	Role string `json:"role"`
	// Email is the contact's email address.
	Email string `json:"email,omitempty"`
	// Name is the contact's name, or empty.
	Name string `json:"name,omitempty"`
	// ExternalID is the application's own ID for the contact, or empty.
	ExternalID string `json:"externalId,omitempty"`
	// CreatedAt is when the contact was added to Intercom.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the contact last changed.
	UpdatedAt time.Time `json:"updatedAt"`
}

// FindContactByEmailOutput lists the contacts with the email address. Intercom can hold several, such
// as a lead and a user for the same person, so the Flow decides which one it means.
type FindContactByEmailOutput struct {
	// Contacts lists at most ContactLimit matching contacts.
	Contacts []Contact `json:"contacts"`
	// HasMoreContacts reports that more contacts than Contacts have the email address.
	HasMoreContacts bool `json:"hasMoreContacts,omitempty"`
}

// FindContactByEmailOperation is the findContactByEmail Query.
type FindContactByEmailOperation struct {
	client *Client
}

type intercomContactWire struct {
	Type       string      `json:"type"`
	ID         flexibleID  `json:"id"`
	Role       string      `json:"role"`
	Email      string      `json:"email"`
	Name       string      `json:"name"`
	ExternalID string      `json:"external_id"`
	CreatedAt  unixSeconds `json:"created_at"`
	UpdatedAt  unixSeconds `json:"updated_at"`
}

// Definition returns the immutable connector operation definition.
func (FindContactByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindContactByEmailDefinition
}

// Invoke reads one page of POST /contacts/search filtered by email. Intercom excludes merged contacts
// from search and can take a few minutes to index a new contact.
func (operation FindContactByEmailOperation) Invoke(call sdkgo.Call, input FindContactByEmailInput) sdkgo.QueryAttempt[FindContactByEmailOutput] {
	if err := validateFindContactByEmailInput(input); err != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchDefect, FindContactByEmailOutput{}, intercomFailurePointer(sdkgo.FailureValidation, findContactByEmailOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, findContactByEmailOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchDefect, FindContactByEmailOutput{}, failure, sdkgo.Receipt{})
	}
	contactLimit := input.ContactLimit
	if contactLimit == 0 {
		contactLimit = DefaultContactLimit
	}
	result := operation.client.exchange(call, credentials, findContactByEmailOperation, intercomRequest{
		method: http.MethodPost, path: "/contacts/search",
		payload: searchRequestWire{
			Query:      searchFilterWire{Field: "email", Operator: "=", Value: input.Email},
			Pagination: searchPaginationWire{PerPage: contactLimit},
		},
	})
	receipt := operation.client.receipt(call, result.response, "")
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewQueryRetry[FindContactByEmailOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(FindContactByEmailBranchInvalidResponse, FindContactByEmailOutput{}, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(FindContactByEmailBranchDefect, FindContactByEmailOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(FindContactByEmailBranchProviderRejected, FindContactByEmailOutput{}, &result.failure, receipt)
	}
	output, err := decodeContactPage(result.response.body, contactLimit)
	if err != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchInvalidResponse, FindContactByEmailOutput{}, intercomFailurePointer(sdkgo.FailureProtocol, findContactByEmailOperation, "Intercom returned an invalid contact page: "+err.Error()), receipt)
	}
	if len(output.Contacts) == 0 {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchNotFound, output, intercomFailurePointer(sdkgo.FailureNotFound, findContactByEmailOperation, "no Intercom contact has the email address"), receipt)
	}
	return sdkgo.NewQueryBranch(FindContactByEmailBranchFound, output, nil, receipt)
}

func validateFindContactByEmailInput(input FindContactByEmailInput) error {
	if !isBareEmailAddress(input.Email) {
		return errors.New("email must be one bare email address such as jane@example.com")
	}
	if input.ContactLimit < 0 || input.ContactLimit > MaxContactLimit {
		return fmt.Errorf("contactLimit must be between 1 and %d, or zero for %d", MaxContactLimit, DefaultContactLimit)
	}
	return nil
}

func decodeContactPage(body []byte, contactLimit int) (FindContactByEmailOutput, error) {
	var page struct {
		Type       string                `json:"type"`
		Data       []intercomContactWire `json:"data"`
		TotalCount int                   `json:"total_count"`
		Pages      *searchPagesWire      `json:"pages"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return FindContactByEmailOutput{}, errors.New("contact page is not a contact list")
	}
	if page.Type != "" && page.Type != "list" {
		return FindContactByEmailOutput{}, errors.New("contact page is not a contact list")
	}
	if page.Data == nil {
		return FindContactByEmailOutput{}, errors.New("contact page has no data array")
	}
	if len(page.Data) > contactLimit {
		return FindContactByEmailOutput{}, errors.New("contact page is larger than requested")
	}
	output := FindContactByEmailOutput{Contacts: make([]Contact, 0, len(page.Data))}
	for index, wire := range page.Data {
		if wire.Type != "" && wire.Type != "contact" {
			return FindContactByEmailOutput{}, fmt.Errorf("contact %d is not a contact", index)
		}
		if !contactIDPattern.MatchString(string(wire.ID)) {
			return FindContactByEmailOutput{}, fmt.Errorf("contact %d has no valid ID", index)
		}
		output.Contacts = append(output.Contacts, Contact{
			ID: string(wire.ID), Role: wire.Role, Email: wire.Email, Name: wire.Name, ExternalID: wire.ExternalID,
			CreatedAt: wire.CreatedAt.time(), UpdatedAt: wire.UpdatedAt.time(),
		})
	}
	hasNextPage := page.Pages != nil && page.Pages.Next != nil && page.Pages.Next.StartingAfter != ""
	output.HasMoreContacts = hasNextPage || page.TotalCount > len(output.Contacts)
	return output, nil
}
