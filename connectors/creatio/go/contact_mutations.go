// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateContactInput supplies a name and email for a new contact.
type CreateContactInput struct {
	// Name is the required full name.
	Name string `json:"name"`
	// Email is an optional address used for read-back after an unknown outcome.
	Email string `json:"email,omitempty"`
}

// UpdateContactInput replaces selected contact fields.
type UpdateContactInput struct {
	// ContactID identifies an existing contact.
	ContactID string `json:"contactId"`
	// Name replaces the full name when set.
	Name *string `json:"name,omitempty"`
	// Email replaces the email when set; an empty string clears it.
	Email *string `json:"email,omitempty"`
}

// DeleteContactInput identifies a contact to delete.
type DeleteContactInput struct {
	// ContactID is the Contact.Id UUID.
	ContactID string `json:"contactId"`
}

// ContactWriteOutput confirms the target UUID of an update or deletion.
type ContactWriteOutput struct {
	// ID is the target Contact.Id UUID.
	ID string `json:"id"`
}

// CreateContactOperation creates one contact with bounded sends.
type CreateContactOperation struct{ client *Client }

// UpdateContactOperation changes selected fields of one contact.
type UpdateContactOperation struct{ client *Client }

// DeleteContactOperation removes one contact.
type DeleteContactOperation struct{ client *Client }

// CreateContact binds the mutation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// UpdateContact binds the mutation to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// DeleteContact binds the mutation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the generated mutation contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// Definition returns the generated mutation contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// Definition returns the generated mutation contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey correlates bounded sends; Creatio does not deduplicate this key.
func (CreateContactOperation) IdempotencyKey(id sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable absolute update.
func (UpdateContactOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable deletion.
func (DeleteContactOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke creates a contact and reads back an email match before resending.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[ContactSummary] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || len(input.Email) > 254 || strings.ContainsAny(input.Email, "\r\n") {
		return createContactBranch(CreateContactBranchDefect, sdkgo.FailureValidation, "invalid contact name or email")
	}
	fence, err := singledispatch.NewFence(call, CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createContactBranch(CreateContactBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createContactUncertain("previous creation cannot be confirmed")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return createContactBranch(CreateContactBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed {
		if decision.Marker.ProviderID != "" {
			response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Contact("+decision.Marker.ProviderID+")?$select=Id%2CName%2CEmail", nil)
			if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
				return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", sdkgo.FailureAvailability, "Creatio contact read-back unavailable"), response.retryAfter)
			}
			if response.status != http.StatusOK {
				return createContactUncertain("saved contact ID cannot be confirmed")
			}
			contact, decodeErr := decodeContactRecord(response.body)
			if decodeErr != nil || !strings.EqualFold(contact.ID, decision.Marker.ProviderID) {
				return createContactUncertain("saved contact ID returned an invalid contact")
			}
			return sdkgo.NewMutationBranch(CreateContactBranchCreated, contact, nil, sdkgo.Receipt{})
		}
		if input.Email != "" {
			matching, known, retry := operation.client.findContactByNameAndEmail(call, token, input.Name, input.Email)
			if retry {
				return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", sdkgo.FailureAvailability, "Creatio contact read-back unavailable"), 0)
			}
			if matching != nil {
				if strings.EqualFold(matching.ID, decision.Marker.ProviderID) {
					return sdkgo.NewMutationBranch(CreateContactBranchCreated, *matching, nil, sdkgo.Receipt{})
				}
				return createContactUncertain("a matching contact cannot prove this creation")
			}
			if !known {
				return createContactUncertain("previous creation cannot be confirmed")
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return createContactUncertain("previous creation cannot be confirmed")
		}
	}
	body, marshalErr := json.Marshal(struct {
		Name  string `json:"Name"`
		Email string `json:"Email,omitempty"`
	}{input.Name, input.Email})
	if marshalErr != nil {
		return createContactBranch(CreateContactBranchDefect, sdkgo.FailureLocalDefect, "contact request could not be encoded")
	}
	var response odataResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doODataRequest(requestContext, token, http.MethodPost, "/0/odata/Contact", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", kind, "Creatio did not create the contact"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", sdkgo.FailureTransport, "contact creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return createContactUncertain("an earlier contact creation may have succeeded")
		}
		return createContactBranch(CreateContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	contact, decodeErr := decodeContactRecord(response.body)
	if response.status != http.StatusCreated || decodeErr != nil {
		return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", sdkgo.FailureProtocol, "Creatio returned invalid contact creation JSON"), 0)
	}
	if err := fence.RecordProviderID(contact.ID); err != nil {
		return sdkgo.NewMutationRetry[ContactSummary](contactsFailure("createContact", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateContactBranchCreated, contact, nil, sdkgo.Receipt{})
}

// Invoke patches the named fields without changing other contact properties.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactWriteOutput] {
	if !contactUUIDPattern.MatchString(input.ContactID) || (input.Name == nil && input.Email == nil) ||
		(input.Name != nil && strings.TrimSpace(*input.Name) == "") || (input.Email != nil && strings.ContainsAny(*input.Email, "\r\n")) {
		return contactWriteBranch("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "invalid contact update")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return contactWriteCredentialFailure("updateContact", UpdateContactBranchDefect, err)
	}
	fields := map[string]any{}
	if input.Name != nil {
		fields["Name"] = *input.Name
	}
	if input.Email != nil {
		fields["Email"] = *input.Email
	}
	body, marshalErr := json.Marshal(fields)
	if marshalErr != nil {
		return contactWriteBranch("updateContact", UpdateContactBranchDefect, sdkgo.FailureLocalDefect, "contact update could not be encoded")
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodPatch, "/0/odata/Contact("+input.ContactID+")", body)
	return classifyContactWrite("updateContact", input.ContactID, response, UpdateContactBranchUpdated, UpdateContactBranchNotFound, UpdateContactBranchProviderRejected)
}

// Invoke permanently deletes one Contact record.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[ContactWriteOutput] {
	if !contactUUIDPattern.MatchString(input.ContactID) {
		return contactWriteBranch("deleteContact", DeleteContactBranchDefect, sdkgo.FailureValidation, "invalid contact UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return contactWriteCredentialFailure("deleteContact", DeleteContactBranchDefect, err)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodDelete, "/0/odata/Contact("+input.ContactID+")", nil)
	return classifyContactWrite("deleteContact", input.ContactID, response, DeleteContactBranchDeleted, DeleteContactBranchNotFound, DeleteContactBranchProviderRejected)
}

func (client *Client) findContactByNameAndEmail(call sdkgo.Call, token string, name string, email string) (*ContactSummary, bool, bool) {
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	filter := "Name eq '" + escape(name) + "' and Email eq '" + escape(email) + "'"
	query := url.Values{"$filter": {filter}, "$select": {"Id,Name,Email"}, "$top": {"2"}}
	response := client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Contact?"+query.Encode(), nil)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return nil, false, true
	}
	if len(envelope.Value) == 0 {
		return nil, true, false
	}
	for _, record := range envelope.Value {
		contact, err := decodeContactRecord(record)
		if err != nil {
			return nil, false, true
		}
		if contact.Name == name && strings.EqualFold(contact.Email, email) {
			return &contact, true, false
		}
	}
	return nil, true, false
}

func classifyContactWrite(operation string, targetID string, response odataResponse, successBranch sdkgo.BranchID, notFoundBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID) sdkgo.MutationAttempt[ContactWriteOutput] {
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ContactWriteOutput](contactsFailure(operation, sdkgo.FailureRateLimit, "Creatio rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ContactWriteOutput](contactsFailure(operation, sdkgo.FailureAvailability, "Creatio write is unavailable"), response.retryAfter)
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[ContactWriteOutput](contactsFailure(operation, sdkgo.FailureTransport, "Creatio write response failed"), 0)
	}
	if response.status == http.StatusNotFound {
		return contactWriteBranch(operation, notFoundBranch, sdkgo.FailureNotFound, "Creatio contact was not found")
	}
	if response.status != http.StatusNoContent {
		return contactWriteBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(successBranch, ContactWriteOutput{ID: targetID}, nil, sdkgo.Receipt{})
}

func contactWriteCredentialFailure(operation string, defectBranch sdkgo.BranchID, err error) sdkgo.MutationAttempt[ContactWriteOutput] {
	if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
		return contactWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
	}
	return sdkgo.NewMutationRetry[ContactWriteOutput](contactsFailure(operation, sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
}

func contactWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContactWriteOutput] {
	failure := contactsFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ContactWriteOutput{}, &failure, sdkgo.Receipt{})
}

func createContactBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContactSummary] {
	failure := contactsFailure("createContact", kind, message)
	return sdkgo.NewMutationBranch(branch, ContactSummary{}, &failure, sdkgo.Receipt{})
}

func createContactUncertain(message string) sdkgo.MutationAttempt[ContactSummary] {
	return sdkgo.NewMutationUncertain(ContactSummary{}, contactsFailure("createContact", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
