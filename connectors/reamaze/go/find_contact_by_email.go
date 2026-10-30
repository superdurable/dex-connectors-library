// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const findContactByEmailOperation = "findContactByEmail"

// FindContactByEmailInput names the email address to resolve.
type FindContactByEmailInput struct {
	// Email is one bare address such as jane@example.com. It matches without regard to letter case.
	Email string `json:"email"`
}

// FindContactByEmailOutput is every contact on the first candidate page whose email equals the input.
type FindContactByEmailOutput struct {
	// Contacts lists the matching contacts in Re:amaze's order; Re:amaze keys a contact by email,
	// so there is usually one.
	Contacts []Contact `json:"contacts"`
	// HasMoreCandidates reports that Re:amaze's name-or-email search listed more pages than the one
	// read, so a matching contact may exist beyond it.
	HasMoreCandidates bool `json:"hasMoreCandidates,omitempty"`
}

// Contact is the connector's view of one Re:amaze contact. Contacts belong to the account and
// are shared by its brands; notes and custom data are omitted.
type Contact struct {
	// Name is the contact's name.
	Name string `json:"name,omitempty"`
	// Email is the contact's email address, which Re:amaze uses as its key, such as in
	// GET /contacts/{email}/identities.
	Email string `json:"email"`
	// FriendlyName is the contact's friendly name, or empty.
	FriendlyName string `json:"friendlyName,omitempty"`
}

// FindContactByEmailOperation is the findContactByEmail Query.
type FindContactByEmailOperation struct {
	client *Client
}

type contactWire struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	FriendlyName string `json:"friendly_name"`
}

// Definition returns the immutable connector operation definition.
func (FindContactByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindContactByEmailDefinition
}

// Invoke reads the first page of GET /contacts?q={email}&type=email and keeps the exact matches,
// because Re:amaze's q searches names and emails rather than matching one address.
func (operation FindContactByEmailOperation) Invoke(call sdkgo.Call, input FindContactByEmailInput) sdkgo.QueryAttempt[FindContactByEmailOutput] {
	if !isBareEmailAddress(input.Email) {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchDefect, FindContactByEmailOutput{}, reamazeFailurePointer(sdkgo.FailureValidation, findContactByEmailOperation,
			"email must be one bare email address such as jane@example.com"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[FindContactByEmailOutput](findContactByEmailOperation, FindContactByEmailBranchDefect, err)
	}
	result := operation.client.exchange(call, credentials, findContactByEmailOperation, reamazeRequest{
		method: http.MethodGet, path: "/contacts", query: url.Values{"q": {input.Email}, "type": {"email"}, "page": {"1"}},
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
	output, err := decodeMatchingContacts(result.response.body, input.Email)
	if err != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchInvalidResponse, FindContactByEmailOutput{}, reamazeFailurePointer(sdkgo.FailureProtocol, findContactByEmailOperation, "Re:amaze returned an invalid contact page: "+err.Error()), receipt)
	}
	if len(output.Contacts) == 0 {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchNotFound, output, nil, receipt)
	}
	return sdkgo.NewQueryBranch(FindContactByEmailBranchFound, output, nil, receipt)
}

func decodeMatchingContacts(body []byte, email string) (FindContactByEmailOutput, error) {
	var document struct {
		pageWire
		Contacts *[]contactWire `json:"contacts"`
	}
	if err := json.Unmarshal(body, &document); err != nil || document.Contacts == nil {
		return FindContactByEmailOutput{}, errors.New("response is not a contact page")
	}
	if err := document.pageWire.validate(len(*document.Contacts)); err != nil {
		return FindContactByEmailOutput{}, err
	}
	output := FindContactByEmailOutput{Contacts: []Contact{}, HasMoreCandidates: *document.PageCount > 1}
	for _, wire := range *document.Contacts {
		if strings.EqualFold(wire.Email, email) {
			output.Contacts = append(output.Contacts, Contact{Name: wire.Name, Email: wire.Email, FriendlyName: wire.FriendlyName})
		}
	}
	return output, nil
}
