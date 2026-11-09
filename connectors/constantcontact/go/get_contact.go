// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var contactIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-(?:[a-fA-F0-9]{4}-){3}[a-fA-F0-9]{12}$`)

// GetContactInput selects a contact by the UUID in its V3 API resource.
type GetContactInput struct {
	// ContactID is a non-secret V3 contact UUID.
	ContactID string `json:"contactId"`
}

// ContactMetadata contains one contact's identity and optional email permission state.
type ContactMetadata struct {
	// ContactID is the V3 contact UUID.
	ContactID string `json:"contactId"`
	// EmailAddress is absent for a contact with no email channel.
	EmailAddress string `json:"emailAddress,omitempty"`
	// FirstName is the contact's optional given name.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the contact's optional family name.
	LastName string `json:"lastName,omitempty"`
	// EmailPermission is Constant Contact's permission_to_send state when an email channel exists.
	EmailPermission string `json:"emailPermission,omitempty"`
}

// GetContactOperation reads one contact without exposing other profile fields.
type GetContactOperation struct{ client *Client }

// GetContact binds the Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated Query contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads one contact and classifies retryable and definitive outcomes.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactMetadata] {
	if !contactIDPattern.MatchString(input.ContactID) {
		return queryBranch(GetContactBranchDefect, sdkgo.FailureValidation, "contact ID must be a UUID")
	}
	response := operation.client.get(call, "/contacts/"+input.ContactID)
	if errors.Is(response.err, errCredential) {
		return queryBranch(GetContactBranchDefect, sdkgo.FailureAuthentication, "Constant Contact credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) {
		return sdkgo.NewQueryRetry[ContactMetadata](queryFailure(sdkgo.FailureAvailability, "Constant Contact credentials could not be refreshed"), 0)
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ContactMetadata](queryFailure(sdkgo.FailureTransport, "Constant Contact request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ContactMetadata](queryFailure(sdkgo.FailureRateLimit, "Constant Contact rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ContactMetadata](queryFailure(sdkgo.FailureAvailability, "Constant Contact is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return queryBranch(GetContactBranchNotFound, sdkgo.FailureNotFound, "Constant Contact did not find the contact")
	}
	if response.status != http.StatusOK {
		return queryBranch(GetContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(GetContactBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Constant Contact response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ContactMetadata](queryFailure(sdkgo.FailureTransport, "Constant Contact response was interrupted"), 0)
	}
	contact, err := decodeContact(response.body)
	if err != nil {
		return queryBranch(GetContactBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(GetContactBranchFound, contact, nil, sdkgo.Receipt{})
}

func decodeContact(body []byte) (ContactMetadata, error) {
	var wire struct {
		ContactID    string `json:"contact_id"`
		EmailAddress struct {
			Address          string `json:"address"`
			PermissionToSend string `json:"permission_to_send"`
		} `json:"email_address"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactMetadata{}, err
	}
	if !contactIDPattern.MatchString(wire.ContactID) {
		return ContactMetadata{}, fmt.Errorf("contact identity missing")
	}
	return ContactMetadata{ContactID: wire.ContactID, EmailAddress: wire.EmailAddress.Address, FirstName: wire.FirstName, LastName: wire.LastName, EmailPermission: wire.EmailAddress.PermissionToSend}, nil
}
