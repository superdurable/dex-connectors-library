// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	findContactByEmailOperationID = "findContactByEmail"
	// MaxContactEmailMatches bounds the contacts one email lookup reads; more matches are still ambiguous.
	MaxContactEmailMatches = 10
	// maximumEmailAddressBytes is the RFC 5321 path limit.
	maximumEmailAddressBytes = 254
)

// FindContactByEmailInput identifies a contact by its email address.
type FindContactByEmailInput struct {
	// EmailAddress is one bare address such as jane@example.com. Xero's filter ignores case and
	// accents; the connector then keeps only contacts whose address equals it, ignoring case.
	EmailAddress string `json:"emailAddress"`
	// IncludesArchived also matches ARCHIVED contacts, which cannot be invoiced; false matches only
	// the contacts Xero lists by default.
	IncludesArchived bool `json:"includesArchived,omitempty"`
}

// FindContactByEmailOutput is the lookup result.
type FindContactByEmailOutput struct {
	// EmailAddress echoes the trimmed requested address.
	EmailAddress string `json:"emailAddress"`
	// Contact is the one matching contact on the found branch, and nil otherwise.
	Contact *Contact `json:"contact,omitempty"`
	// Matches lists up to MaxContactEmailMatches contacts on the ambiguous branch.
	Matches []Contact `json:"matches,omitempty"`
}

// FindContactByEmailOperation implements the findContactByEmail Query.
type FindContactByEmailOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (FindContactByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindContactByEmailDefinition
}

// Invoke reads GET /Contacts with where=EmailAddress=="..." and selects found, notFound, or ambiguous.
func (operation FindContactByEmailOperation) Invoke(call sdkgo.Call, input FindContactByEmailInput) sdkgo.QueryAttempt[FindContactByEmailOutput] {
	client := operation.client
	emailAddress := strings.TrimSpace(input.EmailAddress)
	requested := FindContactByEmailOutput{EmailAddress: emailAddress}
	if err := validateEmailAddress(emailAddress); err != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchDefect, requested,
			xeroFailurePointer(sdkgo.FailureValidation, findContactByEmailOperationID, err.Error()), sdkgo.Receipt{})
	}
	query := url.Values{
		"where": {`EmailAddress=="` + emailAddress + `"`}, "page": {"1"}, "pageSize": {strconv.Itoa(MaxContactEmailMatches)},
	}
	if input.IncludesArchived {
		query.Set("includeArchived", "true")
	}
	result := client.exchange(call, findContactByEmailOperationID, xeroRequest{method: http.MethodGet, path: "/Contacts", query: query})
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := queryAttemptForExchange(result, requested, receipt, queryBranches{
		providerRejected: FindContactByEmailBranchProviderRejected, dailyLimitReached: FindContactByEmailBranchDailyLimitReached,
		invalidResponse: FindContactByEmailBranchInvalidResponse, defect: FindContactByEmailBranchDefect,
	}); isTerminal {
		return attempt
	}
	page, err := decodeContactPage(result.response.body, pageBounds{page: 1, pageSize: MaxContactEmailMatches})
	if err != nil {
		return sdkgo.NewQueryBranch(FindContactByEmailBranchInvalidResponse, requested,
			xeroFailurePointer(sdkgo.FailureProtocol, findContactByEmailOperationID, "Xero returned an invalid contact list: "+err.Error()), receipt)
	}
	var matches []Contact
	for _, contact := range page.Contacts {
		if strings.EqualFold(strings.TrimSpace(contact.EmailAddress), emailAddress) &&
			(input.IncludesArchived || contact.Status == ContactStatusActive || contact.Status == "") {
			matches = append(matches, contact)
		}
	}
	switch len(matches) {
	case 0:
		return sdkgo.NewQueryBranch(FindContactByEmailBranchNotFound, requested,
			xeroFailurePointer(sdkgo.FailureNotFound, findContactByEmailOperationID, "no Xero contact has the email address"), receipt)
	case 1:
		requested.Contact = &matches[0]
		receipt.ProviderObjectID = matches[0].ContactID
		return sdkgo.NewQueryBranch(FindContactByEmailBranchFound, requested, nil, receipt)
	default:
		requested.Matches = matches
		return sdkgo.NewQueryBranch(FindContactByEmailBranchAmbiguous, requested,
			xeroFailurePointer(sdkgo.FailureConflict, findContactByEmailOperationID, "several Xero contacts have the email address"), receipt)
	}
}

// validateEmailAddress accepts one bare address that is safe inside a quoted where literal.
func validateEmailAddress(value string) error {
	address, err := mail.ParseAddress(value)
	switch {
	case value == "":
		return errors.New("emailAddress is required")
	case len(value) > maximumEmailAddressBytes:
		return errors.New("emailAddress is longer than 254 bytes")
	case err != nil || address.Name != "" || address.Address != value || strings.ContainsAny(value, " \"\\'()<>,;"):
		return errors.New("emailAddress must be one bare email address such as jane@example.com")
	}
	return nil
}
