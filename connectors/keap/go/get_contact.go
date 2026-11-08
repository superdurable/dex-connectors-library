// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetContactInput identifies one Keap CRM contact.
type GetContactInput struct {
	// ContactID is the exact Keap contact ID.
	ContactID string `json:"contactId"`
}

// ContactProfile contains the selected contact's ID and optional names.
type ContactProfile struct {
	// ID is the Keap contact ID.
	ID string `json:"id"`
	// GivenName is the contact's first name when available.
	GivenName string `json:"givenName"`
	// FamilyName is the contact's surname when available.
	FamilyName string `json:"familyName"`
}

// GetContactOperation reads one contact from Keap CRM v2.
type GetContactOperation struct{ client *Client }

// GetContact binds the Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the manifest-generated Query worker.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads the contact and classifies provider responses.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactProfile] {
	if !validContactID(input.ContactID) {
		return contactBranch(GetContactBranchDefect, sdkgo.FailureValidation, "contactId must contain 1–128 ASCII letters, digits, hyphens, or underscores")
	}
	response := operation.client.get(call, input)
	if errors.Is(response.err, errCredential) {
		return contactBranch(GetContactBranchDefect, sdkgo.FailureAuthentication, "Keap credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ContactProfile](contactFailure(sdkgo.FailureTransport, "Keap request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ContactProfile](contactFailure(sdkgo.FailureRateLimit, "Keap rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ContactProfile](contactFailure(sdkgo.FailureAvailability, "Keap is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return contactBranch(GetContactBranchNotFound, sdkgo.FailureNotFound, "Keap did not find the contact")
	}
	if response.status != http.StatusOK {
		return contactBranch(GetContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Keap rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return contactBranch(GetContactBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Keap response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ContactProfile](contactFailure(sdkgo.FailureTransport, "Keap response was interrupted"), 0)
	}
	profile, err := decodeContact(response.body, input.ContactID)
	if err != nil {
		return contactBranch(GetContactBranchInvalidResponse, sdkgo.FailureProtocol, "Keap returned invalid contact JSON")
	}
	return sdkgo.NewQueryBranch(GetContactBranchFound, profile, nil, sdkgo.Receipt{})
}

func validContactID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodeContact(body []byte, contactID string) (ContactProfile, error) {
	var wire struct {
		ID         string `json:"id"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactProfile{}, err
	}
	if wire.ID != contactID {
		return ContactProfile{}, errors.New("contact ID mismatch")
	}
	return ContactProfile{ID: wire.ID, GivenName: wire.GivenName, FamilyName: wire.FamilyName}, nil
}

func contactFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "keap", Operation: "getContact", Message: message}
}

func contactBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ContactProfile] {
	failure := contactFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ContactProfile{}, &failure, sdkgo.Receipt{})
}
