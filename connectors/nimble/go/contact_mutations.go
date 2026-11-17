// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package nimble

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ContactFieldValue is one value in Nimble's contact field collection.
type ContactFieldValue struct {
	// Value is the field text, such as a name or email address.
	Value string `json:"value"`
	// Modifier is the provider's field modifier; blank uses no modifier.
	Modifier string `json:"modifier,omitempty"`
}

// CreateContactInput creates one person with a name and optional email.
type CreateContactInput struct {
	// FirstName is the person's given name; either name field must be set.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the person's family name; either name field must be set.
	LastName string `json:"lastName,omitempty"`
	// EmailAddress is the contact email; blank omits it.
	EmailAddress string `json:"emailAddress,omitempty"`
}

// CreateContactOperation creates a person contact.
type CreateContactOperation struct{ client *Client }

// CreateContact binds the create Mutation to this client.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// Definition returns the generated create Mutation contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// IdempotencyKey identifies this create call across Dex attempts; Nimble does not accept it as a header.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one contact under a persisted send cap.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	if strings.TrimSpace(input.FirstName) == "" && strings.TrimSpace(input.LastName) == "" {
		return mutationBranch(CreateContactBranchDefect, "createContact", ContactMetadata{}, sdkgo.FailureValidation, "firstName or lastName is required")
	}
	fields := map[string][]ContactFieldValue{}
	if input.FirstName != "" {
		fields["first name"] = []ContactFieldValue{{Value: input.FirstName}}
	}
	if input.LastName != "" {
		fields["last name"] = []ContactFieldValue{{Value: input.LastName}}
	}
	if input.EmailAddress != "" {
		fields["email"] = []ContactFieldValue{{Value: input.EmailAddress}}
	}
	body, err := json.Marshal(struct {
		Fields     map[string][]ContactFieldValue `json:"fields"`
		RecordType string                         `json:"record_type"`
	}{Fields: fields, RecordType: "person"})
	if err != nil {
		return mutationBranch(CreateContactBranchDefect, "createContact", ContactMetadata{}, sdkgo.FailureLocalDefect, "contact body could not be built")
	}
	fence, err := singledispatch.NewFence(call, CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationBranch(CreateContactBranchDefect, "createContact", ContactMetadata{}, sdkgo.FailureLocalDefect, "dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(ContactMetadata{}, queryFailure("createContact", sdkgo.FailureTransport, "an earlier contact create may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	hasEarlierSend := decision.MaximumPriorSends > 0
	var response writeResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendError error
		response, sendError = operation.client.sendWrite(call, requestContext, http.MethodPost, "/contact", body)
		return sendError
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("createContact", sdkgo.FailureAvailability, "contact create was not dispatched"), 0)
	}
	if errors.Is(err, errCredential) {
		if hasEarlierSend {
			return sdkgo.NewMutationUncertain(ContactMetadata{}, queryFailure("createContact", sdkgo.FailureAuthentication, "earlier create may have succeeded; credentials are unavailable"), sdkgo.Receipt{})
		}
		if checkpointError := fence.RecordNotSent(); checkpointError != nil {
			return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("createContact", sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return mutationBranch(CreateContactBranchDefect, "createContact", ContactMetadata{}, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointError := fence.RecordCanResend(); checkpointError != nil {
			return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("createContact", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		failureKind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			failureKind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("createContact", failureKind, "Nimble temporarily rejected the create"), response.retryAfter)
	}
	if response.status == http.StatusOK && err != nil {
		return sdkgo.NewMutationUncertain(ContactMetadata{}, queryFailure("createContact", sdkgo.FailureProtocol, "Nimble accepted the create but its response was unreadable"), sdkgo.Receipt{})
	}
	if err != nil || response.status >= 500 {
		failure := queryFailure("createContact", sdkgo.FailureAvailability, "contact create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[ContactMetadata](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(ContactMetadata{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		if hasEarlierSend {
			return sdkgo.NewMutationUncertain(ContactMetadata{}, queryFailure("createContact", sdkgo.FailureProviderRejection, "an earlier create may have succeeded"), sdkgo.Receipt{})
		}
		return mutationBranch(CreateContactBranchProviderRejected, "createContact", ContactMetadata{}, sdkgo.FailureProviderRejection, "Nimble rejected the create")
	}
	contact, parseError := decodeWrittenContact(response.body)
	if parseError != nil {
		return sdkgo.NewMutationUncertain(ContactMetadata{}, queryFailure("createContact", sdkgo.FailureProtocol, "Nimble accepted the create but did not identify the contact"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateContactBranchCreated, contact, nil, sdkgo.Receipt{})
}

// UpdateContactInput replaces all Nimble contact fields with the supplied field collection.
type UpdateContactInput struct {
	// ContactID is the 24-character Nimble contact identifier.
	ContactID string `json:"contactId"`
	// Fields is the complete replacement field collection; omitted fields are removed.
	Fields map[string][]ContactFieldValue `json:"fields"`
}

// UpdateContactOperation replaces a contact's fields.
type UpdateContactOperation struct{ client *Client }

// UpdateContact binds the update Mutation to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// Definition returns the generated update Mutation contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// IdempotencyKey identifies the absolute replacement across Dex attempts.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces fields and returns the updated contact identity.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactMetadata] {
	if !isContactID(input.ContactID) || len(input.Fields) == 0 {
		return mutationBranch(UpdateContactBranchDefect, "updateContact", ContactMetadata{}, sdkgo.FailureValidation, "contactId and fields are required")
	}
	for fieldName, values := range input.Fields {
		if strings.TrimSpace(fieldName) == "" || len(values) == 0 {
			return mutationBranch(UpdateContactBranchDefect, "updateContact", ContactMetadata{}, sdkgo.FailureValidation, "each field needs a name and values")
		}
	}
	body, err := json.Marshal(struct {
		Fields map[string][]ContactFieldValue `json:"fields"`
	}{input.Fields})
	if err != nil {
		return mutationBranch(UpdateContactBranchDefect, "updateContact", ContactMetadata{}, sdkgo.FailureValidation, "fields cannot be encoded")
	}
	response, err := operation.client.sendWrite(call, call.Context, http.MethodPut, "/contact/"+url.PathEscape(input.ContactID)+"?type=1", body)
	if errors.Is(err, errCredential) {
		return mutationBranch(UpdateContactBranchDefect, "updateContact", ContactMetadata{}, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("updateContact", sdkgo.FailureTransport, "Nimble update failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ContactMetadata](queryFailure("updateContact", sdkgo.FailureAvailability, "Nimble update is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return mutationBranch(UpdateContactBranchNotFound, "updateContact", ContactMetadata{}, sdkgo.FailureNotFound, "contact was not found")
	}
	if response.status != http.StatusOK {
		return mutationBranch(UpdateContactBranchProviderRejected, "updateContact", ContactMetadata{}, sdkgo.FailureProviderRejection, "Nimble rejected the update")
	}
	contact, parseError := decodeWrittenContact(response.body)
	if parseError != nil {
		return mutationBranch(UpdateContactBranchInvalidResponse, "updateContact", ContactMetadata{}, sdkgo.FailureProtocol, "Nimble returned an invalid contact")
	}
	return sdkgo.NewMutationBranch(UpdateContactBranchUpdated, contact, nil, sdkgo.Receipt{})
}

// DeleteContactInput identifies one Nimble contact for regular deletion.
type DeleteContactInput struct {
	// ContactID is the 24-character Nimble contact identifier.
	ContactID string `json:"contactId"`
}

// DeletedContact identifies the removed contact.
type DeletedContact struct {
	// ContactID is the 24-character Nimble contact identifier.
	ContactID string `json:"contactId"`
}

// DeleteContactOperation deletes one contact.
type DeleteContactOperation struct{ client *Client }

// DeleteContact binds the delete Mutation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the generated delete Mutation contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey identifies the repeated delete call.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke requests regular deletion without the destructive force option.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeletedContact] {
	if !isContactID(input.ContactID) {
		return mutationBranch(DeleteContactBranchDefect, "deleteContact", DeletedContact{}, sdkgo.FailureValidation, "invalid contactId")
	}
	response, err := operation.client.sendWrite(call, call.Context, http.MethodDelete, "/contact/"+url.PathEscape(input.ContactID), nil)
	if errors.Is(err, errCredential) {
		return mutationBranch(DeleteContactBranchDefect, "deleteContact", DeletedContact{}, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedContact](queryFailure("deleteContact", sdkgo.FailureTransport, "Nimble deletion failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedContact](queryFailure("deleteContact", sdkgo.FailureAvailability, "Nimble deletion is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return mutationBranch(DeleteContactBranchNotFound, "deleteContact", DeletedContact{}, sdkgo.FailureNotFound, "contact was not found")
	}
	if response.status != http.StatusOK {
		return mutationBranch(DeleteContactBranchProviderRejected, "deleteContact", DeletedContact{}, sdkgo.FailureProviderRejection, "Nimble rejected the deletion")
	}
	return sdkgo.NewMutationBranch(DeleteContactBranchDeleted, DeletedContact{ContactID: input.ContactID}, nil, sdkgo.Receipt{})
}

type writeResponse struct {
	status     int
	body       []byte
	retryAfter time.Duration
}

func (client *Client) sendWrite(call sdkgo.Call, requestContext context.Context, method, path string, body []byte) (writeResponse, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return writeResponse{}, errCredential
	}
	request, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return writeResponse{}, errRequest
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return writeResponse{}, errRequest
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, readErr := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return writeResponse{status: response.StatusCode, body: responseBody, retryAfter: retryAfter}, readErr
}

func decodeWrittenContact(body []byte) (ContactMetadata, error) {
	var wire struct {
		ID     string                         `json:"id"`
		Fields map[string][]ContactFieldValue `json:"fields"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactMetadata{}, err
	}
	if !isContactID(wire.ID) {
		return ContactMetadata{}, fmt.Errorf("contact ID missing")
	}
	first := func(name string) string {
		if len(wire.Fields[name]) > 0 {
			return wire.Fields[name][0].Value
		}
		return ""
	}
	return ContactMetadata{ID: wire.ID, FirstName: first("first name"), LastName: first("last name"), EmailAddress: first("email")}, nil
}

func mutationBranch[T any](branch sdkgo.BranchID, operation string, value T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
