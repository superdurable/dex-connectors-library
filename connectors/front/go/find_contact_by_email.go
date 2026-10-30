// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// FindContactByEmailInput is the email handle to look up.
type FindContactByEmailInput struct {
	// Email is one bare address, such as jane@acme.example.com, sent as Front's alt:email: contact alias.
	Email string `json:"email"`
}

// FindContactByEmailOutput holds the matching contact. Front keeps each handle on at most one contact,
// so Contacts holds one contact on the found branch and none on notFound; it is a list so a Flow can
// treat every desk's lookup alike.
type FindContactByEmailOutput struct {
	// Contacts is the matching contact.
	Contacts []Contact `json:"contacts"`
}

// Contact is a person or organization the company has communicated with.
type Contact struct {
	// ID is the contact ID, such as crd_1y8sp71.
	ID string `json:"id"`
	// Name is the contact's name, or empty.
	Name string `json:"name,omitempty"`
	// Description is the contact's description, or empty.
	Description string `json:"description,omitempty"`
	// Handles lists every handle that reaches the contact, such as its email addresses and phone numbers.
	Handles []ContactHandle `json:"handles,omitempty"`
	// ListNames names the contact lists the contact belongs to.
	ListNames []string `json:"listNames,omitempty"`
	// IsPrivate reports a contact that belongs to one teammate rather than to a team.
	IsPrivate bool `json:"isPrivate,omitempty"`
	// UpdatedAt is when the contact last changed, or nil when Front omits it.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// ContactHandle is one way to reach a contact.
type ContactHandle struct {
	// Handle is the value, such as jane@acme.example.com.
	Handle string `json:"handle"`
	// Source is Front's handle source, such as email, phone, twitter, front_chat, or custom.
	Source string `json:"source"`
}

// FindContactByEmailOperation implements the findContactByEmail Query. Build it with
// Client.FindContactByEmail.
type FindContactByEmailOperation struct{ client *Client }

type frontContactWire struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Handles     []ContactHandle `json:"handles"`
	Lists       []struct {
		Name string `json:"name"`
	} `json:"lists"`
	IsPrivate bool           `json:"is_private"`
	UpdatedAt frontTimestamp `json:"updated_at"`
}

// Definition returns the immutable findContactByEmail operation definition.
func (FindContactByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindContactByEmailDefinition
}

// Invoke reads GET /contacts/alt:email:{email}; a 404 selects notFound.
func (operation FindContactByEmailOperation) Invoke(call sdkgo.Call, input FindContactByEmailInput) sdkgo.QueryAttempt[FindContactByEmailOutput] {
	operationID := FindContactByEmailDefinition.Operation.OperationID
	client := operation.client
	output := FindContactByEmailOutput{Contacts: []Contact{}}
	if err := validateBareEmail("email", input.Email); err != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchDefect, output, failure, sdkgo.Receipt{})
	}
	result := client.exchange(call.Context, credentials, operationID, frontRequest{
		method: http.MethodGet, path: "/contacts/alt:email:" + url.PathEscape(input.Email),
	})
	receipt := client.receipt(call, "")
	switch {
	case result.isRetryable():
		return sdkgo.NewQueryRetry[FindContactByEmailOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(FindContactByEmailBranchNotFound, output, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(FindContactByEmailBranchDefect, output, &result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(FindContactByEmailBranchInvalidResponse, output, &result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return sdkgo.NewQueryBranch(FindContactByEmailBranchProviderRejected, output, &result.failure, receipt)
	}
	contact, err := decodeContact(result.response.body, input.Email)
	if err != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchInvalidResponse, output,
			frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid contact: "+err.Error()), receipt)
	}
	output.Contacts = append(output.Contacts, contact)
	receipt.ProviderObjectID = contact.ID
	return sdkgo.NewQueryBranch(FindContactByEmailBranchFound, output, nil, receipt)
}

// decodeContact requires the contact to carry the requested email handle, compared without case.
func decodeContact(body []byte, email string) (Contact, error) {
	var wire frontContactWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Contact{}, errors.New("the contact is not valid JSON")
	}
	if !contactIDPattern.MatchString(wire.ID) {
		return Contact{}, errors.New("the contact lacks a valid ID")
	}
	hasEmail := false
	for _, handle := range wire.Handles {
		hasEmail = hasEmail || (handle.Source == "email" && strings.EqualFold(handle.Handle, email))
	}
	if !hasEmail {
		return Contact{}, errors.New("the contact does not hold the requested email handle")
	}
	contact := Contact{
		ID: wire.ID, Name: wire.Name, Description: wire.Description, Handles: wire.Handles,
		IsPrivate: wire.IsPrivate, UpdatedAt: wire.UpdatedAt.pointer(),
	}
	for _, list := range wire.Lists {
		if list.Name != "" {
			contact.ListNames = append(contact.ListNames, list.Name)
		}
	}
	return contact, nil
}
