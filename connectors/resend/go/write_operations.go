// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateContactInput replaces a contact's name and global subscription state.
type UpdateContactInput struct {
	// IDOrEmail is the contact ID or email address.
	IDOrEmail string `json:"idOrEmail"`
	// FirstName is the contact's given name; blank clears it.
	FirstName string `json:"firstName"`
	// LastName is the contact's family name; blank clears it.
	LastName string `json:"lastName"`
	// Unsubscribed is the desired global subscription state.
	Unsubscribed bool `json:"unsubscribed"`
}

// DeleteContactInput selects one global contact.
type DeleteContactInput struct {
	// IDOrEmail is the contact ID or email address.
	IDOrEmail string `json:"idOrEmail"`
}

// DeleteDomainInput selects one sending domain.
type DeleteDomainInput struct {
	// ID is the domain ID.
	ID string `json:"id"`
}

// UpdateScheduledEmailInput replaces one email's delivery time.
type UpdateScheduledEmailInput struct {
	// ID is the scheduled email ID.
	ID string `json:"id"`
	// ScheduledAt is an RFC 3339 timestamp in the future.
	ScheduledAt string `json:"scheduledAt"`
}

// CancelScheduledEmailInput selects one scheduled email.
type CancelScheduledEmailInput struct {
	// ID is the scheduled email ID.
	ID string `json:"id"`
}

// ContactReference identifies a changed contact.
type ContactReference struct {
	// ID is the changed contact's stable identifier.
	ID string `json:"id"`
}

// DeletedContact records Resend's deletion acknowledgement.
type DeletedContact struct {
	// Contact is the deleted contact's identifier.
	Contact string `json:"contact"`
	// Deleted confirms that Resend accepted deletion.
	Deleted bool   `json:"deleted"`
}

// DeletedDomain records Resend's deletion acknowledgement.
type DeletedDomain struct {
	// ID is the deleted domain's identifier.
	ID      string `json:"id"`
	// Deleted confirms that Resend accepted deletion.
	Deleted bool   `json:"deleted"`
}

// EmailReference identifies a changed scheduled email.
type EmailReference struct {
	// ID is the changed scheduled email's identifier.
	ID string `json:"id"`
}

// UpdateContactOperation replaces global contact fields.
type UpdateContactOperation struct{ client *Client }

// DeleteContactOperation deletes a global contact.
type DeleteContactOperation struct{ client *Client }

// DeleteDomainOperation deletes a sending domain.
type DeleteDomainOperation struct{ client *Client }

// UpdateScheduledEmailOperation changes a scheduled delivery time.
type UpdateScheduledEmailOperation struct{ client *Client }

// CancelScheduledEmailOperation cancels a scheduled delivery.
type CancelScheduledEmailOperation struct{ client *Client }

// UpdateContact binds the contact update to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// DeleteContact binds the contact deletion to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// DeleteDomain binds the domain deletion to this client.
func (client *Client) DeleteDomain() DeleteDomainOperation { return DeleteDomainOperation{client} }

// UpdateScheduledEmail binds the scheduled-email update to this client.
func (client *Client) UpdateScheduledEmail() UpdateScheduledEmailOperation {
	return UpdateScheduledEmailOperation{client}
}

// CancelScheduledEmail binds cancellation to this client.
func (client *Client) CancelScheduledEmail() CancelScheduledEmailOperation {
	return CancelScheduledEmailOperation{client}
}

// Definition returns the generated operation contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// Definition returns the generated operation contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// Definition returns the generated operation contract.
func (DeleteDomainOperation) Definition() sdkgo.MutationDefinition { return DeleteDomainDefinition }

// Definition returns the generated operation contract.
func (UpdateScheduledEmailOperation) Definition() sdkgo.MutationDefinition {
	return UpdateScheduledEmailDefinition
}

// Definition returns the generated operation contract.
func (CancelScheduledEmailOperation) Definition() sdkgo.MutationDefinition {
	return CancelScheduledEmailDefinition
}

// IdempotencyKey derives a stable call key.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key.
func (DeleteDomainOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteDomainInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key.
func (UpdateScheduledEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateScheduledEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key.
func (CancelScheduledEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelScheduledEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke updates a contact's absolute fields.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactReference] {
	if strings.TrimSpace(input.IDOrEmail) == "" {
		return mutationFailure[ContactReference]("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "contact ID or email is required")
	}
	body, err := json.Marshal(struct {
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		Unsubscribed bool   `json:"unsubscribed"`
	}{input.FirstName, input.LastName, input.Unsubscribed})
	if err != nil {
		return mutationFailure[ContactReference]("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "contact cannot be encoded")
	}
	return runMutation(operation.client, call, "updateContact", http.MethodPatch, "/contacts/"+url.PathEscape(input.IDOrEmail), body, "", "", UpdateContactBranchUpdated, UpdateContactBranchNotFound, UpdateContactBranchProviderRejected, UpdateContactBranchInvalidResponse, UpdateContactBranchDefect, decodeContactReference)
}

// Invoke deletes a contact by ID or email address.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeletedContact] {
	if strings.TrimSpace(input.IDOrEmail) == "" {
		return mutationFailure[DeletedContact]("deleteContact", DeleteContactBranchDefect, sdkgo.FailureValidation, "contact ID or email is required")
	}
	return runMutation(operation.client, call, "deleteContact", http.MethodDelete, "/contacts/"+url.PathEscape(input.IDOrEmail), nil, "", "", DeleteContactBranchDeleted, DeleteContactBranchNotFound, DeleteContactBranchProviderRejected, DeleteContactBranchInvalidResponse, DeleteContactBranchDefect, func(body []byte) (DeletedContact, error) {
		var result DeletedContact
		err := json.Unmarshal(body, &result)
		if err == nil && (!result.Deleted || result.Contact == "") {
			err = fmt.Errorf("deletion acknowledgement missing")
		}
		return result, err
	})
}

// Invoke deletes one domain by ID.
func (operation DeleteDomainOperation) Invoke(call sdkgo.Call, input DeleteDomainInput) sdkgo.MutationAttempt[DeletedDomain] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedDomain]("deleteDomain", DeleteDomainBranchDefect, sdkgo.FailureValidation, "domain ID is required")
	}
	return runMutation(operation.client, call, "deleteDomain", http.MethodDelete, "/domains/"+url.PathEscape(input.ID), nil, "", "", DeleteDomainBranchDeleted, DeleteDomainBranchNotFound, DeleteDomainBranchProviderRejected, DeleteDomainBranchInvalidResponse, DeleteDomainBranchDefect, func(body []byte) (DeletedDomain, error) {
		var result DeletedDomain
		err := json.Unmarshal(body, &result)
		if err == nil && (!result.Deleted || result.ID == "") {
			err = fmt.Errorf("deletion acknowledgement missing")
		}
		return result, err
	})
}

// Invoke replaces a scheduled email's delivery time.
func (operation UpdateScheduledEmailOperation) Invoke(call sdkgo.Call, input UpdateScheduledEmailInput) sdkgo.MutationAttempt[EmailReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[EmailReference]("updateScheduledEmail", UpdateScheduledEmailBranchDefect, sdkgo.FailureValidation, "email ID is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, input.ScheduledAt); err != nil {
		return mutationFailure[EmailReference]("updateScheduledEmail", UpdateScheduledEmailBranchDefect, sdkgo.FailureValidation, "scheduledAt must be RFC 3339")
	}
	body, err := json.Marshal(struct {
		ScheduledAt string `json:"scheduled_at"`
	}{input.ScheduledAt})
	if err != nil {
		return mutationFailure[EmailReference]("updateScheduledEmail", UpdateScheduledEmailBranchDefect, sdkgo.FailureValidation, "schedule cannot be encoded")
	}
	return runMutation(operation.client, call, "updateScheduledEmail", http.MethodPatch, "/emails/"+url.PathEscape(input.ID), body, "", "", UpdateScheduledEmailBranchUpdated, UpdateScheduledEmailBranchNotFound, UpdateScheduledEmailBranchProviderRejected, UpdateScheduledEmailBranchInvalidResponse, UpdateScheduledEmailBranchDefect, decodeEmailReference)
}

// Invoke cancels a scheduled email.
func (operation CancelScheduledEmailOperation) Invoke(call sdkgo.Call, input CancelScheduledEmailInput) sdkgo.MutationAttempt[EmailReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[EmailReference]("cancelScheduledEmail", CancelScheduledEmailBranchDefect, sdkgo.FailureValidation, "email ID is required")
	}
	return runMutation(operation.client, call, "cancelScheduledEmail", http.MethodPost, "/emails/"+url.PathEscape(input.ID)+"/cancel", nil, "", "", CancelScheduledEmailBranchCancelled, CancelScheduledEmailBranchNotFound, CancelScheduledEmailBranchProviderRejected, CancelScheduledEmailBranchInvalidResponse, CancelScheduledEmailBranchDefect, decodeEmailReference)
}

func decodeContactReference(body []byte) (ContactReference, error) {
	var result ContactReference
	err := json.Unmarshal(body, &result)
	if err == nil && result.ID == "" {
		err = fmt.Errorf("contact ID missing")
	}
	return result, err
}
func decodeEmailReference(body []byte) (EmailReference, error) {
	var result EmailReference
	err := json.Unmarshal(body, &result)
	if err == nil && result.ID == "" {
		err = fmt.Errorf("email ID missing")
	}
	return result, err
}
