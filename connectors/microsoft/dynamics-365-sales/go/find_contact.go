// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dynamics365sales

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// FindContactInput selects one contact by its primary email address.
type FindContactInput struct {
	// EmailAddress is the contact's exact primary email address.
	EmailAddress string `json:"emailAddress"`
}

// Contact is the stable identity and basic display information for one Dataverse contact.
type Contact struct {
	// ContactID is the Dataverse contact UUID.
	ContactID string `json:"contactId"`
	// FullName is the name shown in Dynamics 365 Sales.
	FullName string `json:"fullName"`
	// EmailAddress is the primary email returned by Dataverse.
	EmailAddress string `json:"emailAddress"`
}

// FindContactOperation reads a contact by exact email.
type FindContactOperation struct{ client *Client }

// FindContact binds the contact Query to this client.
func (client *Client) FindContact() FindContactOperation { return FindContactOperation{client} }

// Definition returns the generated contact Query contract.
func (FindContactOperation) Definition() sdkgo.QueryDefinition { return FindContactDefinition }

// Invoke obtains a token, searches contacts, and classifies the result.
func (operation FindContactOperation) Invoke(call sdkgo.Call, input FindContactInput) sdkgo.QueryAttempt[Contact] {
	email, err := mail.ParseAddress(input.EmailAddress)
	if err != nil || email.Address != input.EmailAddress || strings.ContainsAny(input.EmailAddress, "\r\n") || len(input.EmailAddress) > 320 {
		return contactBranch(FindContactBranchDefect, sdkgo.FailureValidation, "contact email address is invalid")
	}
	response := operation.client.queryContacts(call, input.EmailAddress)
	if errors.Is(response.err, errCredential) {
		return contactBranch(FindContactBranchDefect, sdkgo.FailureAuthentication, "client credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Contact](contactFailure(sdkgo.FailureTransport, "Microsoft request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Contact](contactFailure(sdkgo.FailureRateLimit, "Microsoft rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Contact](contactFailure(sdkgo.FailureAvailability, "Microsoft is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return contactBranch(FindContactBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Microsoft response exceeded the configured limit")
	}
	if errors.Is(response.err, errInvalidToken) {
		return contactBranch(FindContactBranchInvalidResponse, sdkgo.FailureProtocol, "Entra returned an invalid access token")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Contact](contactFailure(sdkgo.FailureTransport, "Microsoft response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return contactBranch(FindContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Microsoft rejected HTTP "+strconv.Itoa(response.status))
	}
	var payload struct {
		Value []struct {
			ContactID    string `json:"contactid"`
			FullName     string `json:"fullname"`
			EmailAddress string `json:"emailaddress1"`
		} `json:"value"`
	}
	if err := json.Unmarshal(response.body, &payload); err != nil || payload.Value == nil || len(payload.Value) > 2 {
		return contactBranch(FindContactBranchInvalidResponse, sdkgo.FailureProtocol, "Dataverse returned invalid contacts JSON")
	}
	if len(payload.Value) == 0 {
		return sdkgo.NewQueryBranch(FindContactBranchMissing, Contact{}, nil, sdkgo.Receipt{})
	}
	if len(payload.Value) > 1 {
		return sdkgo.NewQueryBranch(FindContactBranchMultipleMatches, Contact{}, nil, sdkgo.Receipt{})
	}
	wireContact := payload.Value[0]
	if !identifierPattern.MatchString(wireContact.ContactID) || wireContact.EmailAddress == "" || !strings.EqualFold(wireContact.EmailAddress, input.EmailAddress) {
		return contactBranch(FindContactBranchInvalidResponse, sdkgo.FailureProtocol, "Dataverse returned an invalid contact")
	}
	contact := Contact{ContactID: wireContact.ContactID, FullName: wireContact.FullName, EmailAddress: wireContact.EmailAddress}
	return sdkgo.NewQueryBranch(FindContactBranchFound, contact, nil, sdkgo.Receipt{})
}

func contactFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "microsoft-dynamics-365-sales", Operation: "findContact", Message: message}
}

func contactBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Contact] {
	failure := contactFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, Contact{}, &failure, sdkgo.Receipt{})
}
