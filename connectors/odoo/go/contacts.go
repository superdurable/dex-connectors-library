// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Contact is the public identity and communication fields of an Odoo partner.
type Contact struct {
	// ID is the positive Odoo record ID.
	ID int64 `json:"id"`
	// Name is the partner display name.
	Name string `json:"name"`
	// Email is the contact email, when set.
	Email string `json:"email"`
	// Phone is the contact phone, when set.
	Phone string `json:"phone"`
}

// ContactsPage contains one bounded page of contacts.
type ContactsPage struct {
	// Contacts are the records in Odoo's search order.
	Contacts []Contact `json:"contacts"`
}

// ListContactsInput selects a bounded contact page.
type ListContactsInput struct {
	// Limit is one through one hundred.
	Limit int `json:"limit"`
	// Offset skips this many records.
	Offset int `json:"offset"`
}

// GetContactInput selects a contact by Odoo record ID.
type GetContactInput struct {
	// ID is a positive contact record ID.
	ID int64 `json:"id"`
}

// CreateContactInput supplies one contact's communication fields.
type CreateContactInput struct {
	// Name is the required contact name.
	Name string `json:"name"`
	// Email is an optional contact email used for read-back.
	Email string `json:"email"`
	// Phone is an optional contact phone.
	Phone string `json:"phone"`
}

// CreatedContact identifies the new contact.
type CreatedContact struct {
	// ID is the created Odoo record ID.
	ID int64 `json:"id"`
}

// UpdateContactInput sets a contact's name, email, and phone to absolute values.
type UpdateContactInput struct {
	// ID is a positive contact record ID.
	ID int64 `json:"id"`
	// Name is the new required contact name.
	Name string `json:"name"`
	// Email is the new email; blank clears it.
	Email string `json:"email"`
	// Phone is the new phone; blank clears it.
	Phone string `json:"phone"`
}

// UpdatedContact identifies the updated contact.
type UpdatedContact struct {
	// ID is the updated Odoo record ID.
	ID int64 `json:"id"`
}

// DeleteContactInput selects a contact to delete.
type DeleteContactInput struct {
	// ID is a positive contact record ID.
	ID int64 `json:"id"`
}

// DeletedContact identifies the deleted contact.
type DeletedContact struct {
	// ID is the deleted Odoo record ID.
	ID int64 `json:"id"`
}

func (client *Client) callModel(call sdkgo.Call, requestContext context.Context, model, method string, body []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, client.baseURL+"/json/2/"+model+"/"+method, bytes.NewReader(body))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("X-Odoo-Database", client.database)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, readError := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{status: response.StatusCode, body: responseBody, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: readError}
}

func contactFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "odoo", Operation: operation, Message: message}
}

func contactQueryBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := contactFailure(operation, kind, message)
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

type contactQueryBranches struct {
	found    sdkgo.BranchID
	notFound sdkgo.BranchID
	rejected sdkgo.BranchID
	invalid  sdkgo.BranchID
	defect   sdkgo.BranchID
}

func invokeContactQuery[T any](call sdkgo.Call, client *Client, operation, method string, body []byte, branches contactQueryBranches, decode func([]byte) (T, bool, error)) sdkgo.QueryAttempt[T] {
	response := client.callModel(call, call.Context, "res.partner", method, body)
	if errors.Is(response.err, errCredential) {
		return contactQueryBranch[T](operation, branches.defect, sdkgo.FailureAuthentication, "Odoo credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](contactFailure(operation, sdkgo.FailureTransport, "Odoo request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](contactFailure(operation, sdkgo.FailureRateLimit, "Odoo rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](contactFailure(operation, sdkgo.FailureAvailability, "Odoo is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return contactQueryBranch[T](operation, branches.rejected, sdkgo.FailureProviderRejection, rejectedStatus(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return contactQueryBranch[T](operation, branches.invalid, sdkgo.FailureResponseTooLarge, "Odoo response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](contactFailure(operation, sdkgo.FailureTransport, "Odoo response was interrupted"), 0)
	}
	value, found, err := decode(response.body)
	if err != nil {
		return contactQueryBranch[T](operation, branches.invalid, sdkgo.FailureProtocol, "Odoo returned invalid contact data")
	}
	if !found {
		return contactQueryBranch[T](operation, branches.notFound, sdkgo.FailureNotFound, "Odoo did not find the contact")
	}
	return sdkgo.NewQueryBranch(branches.found, value, nil, sdkgo.Receipt{})
}

func decodeContacts(body []byte) ([]Contact, error) {
	var wire []struct {
		ID    int64           `json:"id"`
		Name  string          `json:"name"`
		Email json.RawMessage `json:"email"`
		Phone json.RawMessage `json:"phone"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire == nil {
		return nil, errors.New("contact list is invalid")
	}
	contacts := make([]Contact, 0, len(wire))
	for _, record := range wire {
		if record.ID <= 0 || record.Name == "" {
			return nil, errors.New("contact ID or name is invalid")
		}
		email, err := optionalOdooString(record.Email)
		if err != nil {
			return nil, err
		}
		phone, err := optionalOdooString(record.Phone)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, Contact{ID: record.ID, Name: record.Name, Email: email, Phone: phone})
	}
	return contacts, nil
}

func optionalOdooString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "false" || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

// ListContactsOperation lists contact records.
type ListContactsOperation struct{ client *Client }

// ListContacts binds the contact list Query.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// Definition returns the generated contact list contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Invoke lists one bounded contact page.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	branches := contactQueryBranches{found: ListContactsBranchListed, rejected: ListContactsBranchProviderRejected, invalid: ListContactsBranchInvalidResponse, defect: ListContactsBranchDefect}
	if input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return contactQueryBranch[ContactsPage]("listContacts", branches.defect, sdkgo.FailureValidation, "limit or offset is invalid")
	}
	body, _ := json.Marshal(map[string]any{"domain": []any{}, "fields": []string{"id", "name", "email", "phone"}, "limit": input.Limit, "offset": input.Offset})
	return invokeContactQuery(call, operation.client, "listContacts", "search_read", body, branches, func(body []byte) (ContactsPage, bool, error) {
		contacts, err := decodeContacts(body)
		if len(contacts) > input.Limit {
			return ContactsPage{}, false, errors.New("contact page exceeds limit")
		}
		return ContactsPage{Contacts: contacts}, true, err
	})
}

// GetContactOperation reads a contact.
type GetContactOperation struct{ client *Client }

// GetContact binds the contact read Query.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated contact read contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke reads one contact by ID.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[Contact] {
	branches := contactQueryBranches{found: GetContactBranchFound, notFound: GetContactBranchNotFound, rejected: GetContactBranchProviderRejected, invalid: GetContactBranchInvalidResponse, defect: GetContactBranchDefect}
	if input.ID <= 0 {
		return contactQueryBranch[Contact]("getContact", branches.defect, sdkgo.FailureValidation, "contact ID is invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}, "fields": []string{"id", "name", "email", "phone"}})
	return invokeContactQuery(call, operation.client, "getContact", "read", body, branches, func(body []byte) (Contact, bool, error) {
		contacts, err := decodeContacts(body)
		if err != nil {
			return Contact{}, false, err
		}
		if len(contacts) == 0 {
			return Contact{}, false, nil
		}
		if len(contacts) != 1 || contacts[0].ID != input.ID {
			return Contact{}, false, errors.New("unexpected contact ID")
		}
		return contacts[0], true, nil
	})
}

func contactMutationBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := contactFailure(operation, kind, message)
	var zero T
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

// CreateContactOperation creates a contact with limited repeat sends.
type CreateContactOperation struct{ client *Client }

// CreateContact binds contact creation.
func (client *Client) CreateContact() CreateContactOperation { return CreateContactOperation{client} }

// Definition returns the generated contact creation contract.
func (CreateContactOperation) Definition() sdkgo.MutationDefinition { return CreateContactDefinition }

// IdempotencyKey keeps a stable call identity; Odoo has no idempotency header.
func (CreateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one contact, reading back by exact email and name after an ambiguous send.
func (operation CreateContactOperation) Invoke(call sdkgo.Call, input CreateContactInput) sdkgo.MutationAttempt[CreatedContact] {
	if !validContactWrite(input.Name, input.Email, input.Phone) {
		return contactMutationBranch[CreatedContact]("createContact", CreateContactBranchDefect, sdkgo.FailureValidation, "contact fields are invalid")
	}
	body, _ := json.Marshal(map[string]any{"vals_list": []any{map[string]any{"name": input.Name, "email": input.Email, "phone": input.Phone}}})
	fence, err := singledispatch.NewFence(call, CreateContactDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return contactMutationBranch[CreatedContact]("createContact", CreateContactBranchDefect, sdkgo.FailureLocalDefect, "dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.MaximumPriorSends > 0 || decision.Kind != singledispatch.SendAllowed {
		contact, found, readError := operation.client.readBackContact(call, input)
		if found {
			return sdkgo.NewMutationBranch(CreateContactBranchCreated, CreatedContact{ID: contact.ID}, nil, sdkgo.Receipt{})
		}
		if readError != nil {
			return sdkgo.NewMutationRetry[CreatedContact](contactFailure("createContact", sdkgo.FailureAvailability, "contact read-back is unavailable"), 0)
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CreatedContact{}, contactFailure("createContact", sdkgo.FailureTransport, "contact creation may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.callModel(call, requestContext, "res.partner", "create", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CreatedContact](contactFailure("createContact", sdkgo.FailureAvailability, "contact was not dispatched"), 0)
	}
	if errors.Is(err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedContact{}, contactFailure("createContact", sdkgo.FailureAuthentication, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[CreatedContact](contactFailure("createContact", sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return contactMutationBranch[CreatedContact]("createContact", CreateContactBranchDefect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CreatedContact](contactFailure("createContact", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CreatedContact](contactFailure("createContact", kind, "Odoo temporarily rejected the create"), response.retryAfter)
	}
	if err != nil || response.status >= 500 {
		failure := contactFailure("createContact", sdkgo.FailureAvailability, "contact create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[CreatedContact](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(CreatedContact{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedContact{}, contactFailure("createContact", sdkgo.FailureProviderRejection, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		return contactMutationBranch[CreatedContact]("createContact", CreateContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Odoo rejected contact creation")
	}
	var ids []int64
	if json.Unmarshal(response.body, &ids) != nil || len(ids) != 1 || ids[0] <= 0 {
		return sdkgo.NewMutationUncertain(CreatedContact{}, contactFailure("createContact", sdkgo.FailureProtocol, "Odoo accepted contact creation without a valid ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateContactBranchCreated, CreatedContact{ID: ids[0]}, nil, sdkgo.Receipt{})
}

func (client *Client) readBackContact(call sdkgo.Call, input CreateContactInput) (Contact, bool, error) {
	if input.Email == "" {
		return Contact{}, false, nil
	}
	body, _ := json.Marshal(map[string]any{"domain": [][]any{{"email", "=", input.Email}, {"name", "=", input.Name}}, "fields": []string{"id", "name", "email", "phone"}, "limit": 2})
	response := client.callModel(call, call.Context, "res.partner", "search_read", body)
	if response.err != nil || response.status != http.StatusOK {
		return Contact{}, false, errors.New("contact read-back failed")
	}
	contacts, err := decodeContacts(response.body)
	if err != nil {
		return Contact{}, false, err
	}
	if len(contacts) == 1 && contacts[0].Email == input.Email && contacts[0].Name == input.Name && contacts[0].Phone == input.Phone {
		return contacts[0], true, nil
	}
	return Contact{}, false, nil
}

func validContactWrite(name, email, phone string) bool {
	return validNamePattern(name) && len(email) <= 320 && len(phone) <= 64 && !strings.ContainsAny(email+phone, "\r\n\x00")
}

func invokeContactBooleanMutation[T any](call sdkgo.Call, client *Client, operation, method string, body []byte, id int64, success, notFound, rejected, invalid, defect sdkgo.BranchID, value T) sdkgo.MutationAttempt[T] {
	response := client.callModel(call, call.Context, "res.partner", method, body)
	if errors.Is(response.err, errCredential) {
		return contactMutationBranch[T](operation, defect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[T](contactFailure(operation, sdkgo.FailureTransport, "Odoo request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](contactFailure(operation, sdkgo.FailureRateLimit, "Odoo rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[T](contactFailure(operation, sdkgo.FailureAvailability, "Odoo is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return contactMutationBranch[T](operation, notFound, sdkgo.FailureNotFound, "Odoo did not find the contact")
	}
	if response.status != http.StatusOK {
		return contactMutationBranch[T](operation, rejected, sdkgo.FailureProviderRejection, rejectedStatus(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return contactMutationBranch[T](operation, invalid, sdkgo.FailureResponseTooLarge, "Odoo response exceeded the limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[T](contactFailure(operation, sdkgo.FailureTransport, "Odoo response was interrupted"), 0)
	}
	var succeeded bool
	if json.Unmarshal(response.body, &succeeded) != nil {
		return contactMutationBranch[T](operation, invalid, sdkgo.FailureProtocol, "Odoo returned invalid write result")
	}
	if !succeeded {
		return contactMutationBranch[T](operation, notFound, sdkgo.FailureNotFound, fmt.Sprintf("Odoo did not update contact %d", id))
	}
	return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
}

// UpdateContactOperation sets a contact's communication fields.
type UpdateContactOperation struct{ client *Client }

// UpdateContact binds contact replacement.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// Definition returns the generated update contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// IdempotencyKey identifies an absolute field replacement across retries.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the name, email, and phone to supplied absolute values.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[UpdatedContact] {
	if input.ID <= 0 || !validContactWrite(input.Name, input.Email, input.Phone) {
		return contactMutationBranch[UpdatedContact]("updateContact", UpdateContactBranchDefect, sdkgo.FailureValidation, "contact fields are invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}, "vals": map[string]any{"name": input.Name, "email": input.Email, "phone": input.Phone}})
	return invokeContactBooleanMutation(call, operation.client, "updateContact", "write", body, input.ID, UpdateContactBranchUpdated, UpdateContactBranchNotFound, UpdateContactBranchProviderRejected, UpdateContactBranchInvalidResponse, UpdateContactBranchDefect, UpdatedContact{ID: input.ID})
}

// DeleteContactOperation deletes a contact.
type DeleteContactOperation struct{ client *Client }

// DeleteContact binds contact deletion.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the generated deletion contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey identifies repeated deletion of the same contact.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the selected contact.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeletedContact] {
	if input.ID <= 0 {
		return contactMutationBranch[DeletedContact]("deleteContact", DeleteContactBranchDefect, sdkgo.FailureValidation, "contact ID is invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}})
	return invokeContactBooleanMutation(call, operation.client, "deleteContact", "unlink", body, input.ID, DeleteContactBranchDeleted, DeleteContactBranchNotFound, DeleteContactBranchProviderRejected, DeleteContactBranchInvalidResponse, DeleteContactBranchDefect, DeletedContact{ID: input.ID})
}
