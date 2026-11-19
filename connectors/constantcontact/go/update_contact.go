// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateContactInput changes selected core fields without changing email consent.
type UpdateContactInput struct {
	// ContactID is the existing V3 contact UUID.
	ContactID string `json:"contactId"`
	// UpdateSource is Account or Contact, identifying who supplied this update.
	UpdateSource string `json:"updateSource"`
	// FirstName changes the given name when non-nil; an empty value clears it.
	FirstName *string `json:"firstName,omitempty"`
	// LastName changes the family name when non-nil; an empty value clears it.
	LastName *string `json:"lastName,omitempty"`
	// JobTitle changes the job title when non-nil; an empty value clears it.
	JobTitle *string `json:"jobTitle,omitempty"`
	// CompanyName changes the company name when non-nil; an empty value clears it.
	CompanyName *string `json:"companyName,omitempty"`
}

// UpdateContactOperation preserves existing core fields during a name or company update.
type UpdateContactOperation struct{ client *Client }

// UpdateContact binds the Mutation to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// IdempotencyKey correlates attempts of an absolute core-field replacement.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fetches current core fields, overlays named changes, and sends a complete core update.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	if !contactIDPattern.MatchString(input.ContactID) ||
		(input.UpdateSource != "Account" && input.UpdateSource != "Contact") ||
		(input.FirstName == nil && input.LastName == nil && input.JobTitle == nil && input.CompanyName == nil) ||
		!validContactCoreField(input.FirstName) || !validContactCoreField(input.LastName) ||
		!validContactCoreField(input.JobTitle) || !validContactCoreField(input.CompanyName) {
		return updateContactBranch(UpdateContactBranchDefect, sdkgo.FailureValidation, "invalid contact ID, source, or changed fields")
	}
	response := operation.client.get(call, "/contacts/"+input.ContactID)
	if kind, failure := classifyListResponse("updateContact", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewMutationRetry[ContactMetadata](failure, response.retryAfter)
		}
		branch := UpdateContactBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdateContactBranchNotFound
		} else if kind == listResponseInvalid {
			branch = UpdateContactBranchInvalidResponse
		} else if kind == listResponseDefect {
			branch = UpdateContactBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, ContactMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return updateContactBranch(UpdateContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	var current contactCoreUpdate
	if json.Unmarshal(response.body, &current) != nil || current.ContactID != input.ContactID ||
		(current.EmailAddress != nil && (current.EmailAddress.Address == "" || current.EmailAddress.PermissionToSend == "")) {
		return updateContactBranch(UpdateContactBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned invalid contact core fields")
	}
	if input.FirstName != nil {
		current.FirstName = *input.FirstName
	}
	if input.LastName != nil {
		current.LastName = *input.LastName
	}
	if input.JobTitle != nil {
		current.JobTitle = *input.JobTitle
	}
	if input.CompanyName != nil {
		current.CompanyName = *input.CompanyName
	}
	current.UpdateSource = input.UpdateSource
	current.ContactID = ""
	requestBody, err := json.Marshal(current)
	if err != nil {
		return updateContactBranch(UpdateContactBranchDefect, sdkgo.FailureLocalDefect, "contact update could not be encoded")
	}
	response = operation.client.send(call, http.MethodPut, "/contacts/"+input.ContactID, requestBody)
	if kind, failure := classifyListResponse("updateContact", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewMutationRetry[ContactMetadata](failure, response.retryAfter)
		}
		branch := UpdateContactBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdateContactBranchNotFound
		} else if kind == listResponseInvalid {
			branch = UpdateContactBranchInvalidResponse
		} else if kind == listResponseDefect {
			branch = UpdateContactBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, ContactMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return updateContactBranch(UpdateContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	updated, err := decodeContact(response.body)
	if err != nil || updated.ContactID != input.ContactID {
		return sdkgo.NewMutationRetry[ContactMetadata](listFailure("updateContact", sdkgo.FailureProtocol, "Constant Contact returned an invalid updated contact"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateContactBranchUpdated, updated, nil, sdkgo.Receipt{})
}

type contactCoreUpdate struct {
	ContactID    string `json:"contact_id,omitempty"`
	EmailAddress *struct {
		Address          string `json:"address"`
		PermissionToSend string `json:"permission_to_send"`
	} `json:"email_address,omitempty"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	JobTitle      string `json:"job_title"`
	CompanyName   string `json:"company_name"`
	BirthdayMonth *int   `json:"birthday_month,omitempty"`
	BirthdayDay   *int   `json:"birthday_day,omitempty"`
	Anniversary   string `json:"anniversary,omitempty"`
	UpdateSource  string `json:"update_source"`
}

func validContactCoreField(value *string) bool {
	return value == nil || len(*value) <= 50 && !strings.ContainsAny(*value, "\r\n")
}

func updateContactBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContactMetadata] {
	failure := listFailure("updateContact", kind, message)
	return sdkgo.NewMutationBranch(branch, ContactMetadata{}, &failure, sdkgo.Receipt{})
}
