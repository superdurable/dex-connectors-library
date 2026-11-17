// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListContactsInput selects a bounded page of contacts.
type ListContactsInput struct {
	// Limit is the number of contacts from 1 to 1000; zero uses 50.
	Limit int `json:"limit"`
	// Offset is the zero-based starting index.
	Offset int `json:"offset"`
}

// ContactsList contains minimal contact identities and the account's total count.
type ContactsList struct {
	// Contacts is the current page without attributes or statistics.
	Contacts []ContactIdentity `json:"contacts"`
	// Count is Brevo's total matching contact count.
	Count int `json:"count"`
}

// ContactIdentity identifies an email or phone-only contact in a list.
type ContactIdentity struct {
	// ID is Brevo's contact identifier.
	ID int64 `json:"id"`
	// Email is present for email-identified contacts.
	Email string `json:"email,omitempty"`
}

// UpdateContactInput sets absolute existing contact attributes by email.
type UpdateContactInput struct {
	// Email identifies the contact.
	Email string `json:"email"`
	// Attributes maps uppercase Brevo names to absolute string values.
	Attributes map[string]string `json:"attributes"`
}

// DeleteContactInput identifies a contact to delete by email.
type DeleteContactInput struct {
	// Email identifies the contact.
	Email string `json:"email"`
}

// ContactChangeResult identifies the updated or deleted contact.
type ContactChangeResult struct {
	// Email is the submitted address.
	Email string `json:"email"`
}

// ListAttributesInput requests the account's contact attribute definitions.
type ListAttributesInput struct{}

// AttributeIdentity describes a contact attribute without its values.
type AttributeIdentity struct {
	// Name is the Brevo attribute name.
	Name string `json:"name"`
	// Category identifies the attribute group.
	Category string `json:"category"`
	// Type identifies the attribute value type.
	Type string `json:"type"`
}

// AttributesList contains the account's attribute definitions.
type AttributesList struct {
	// Attributes contains names, categories, and types only.
	Attributes []AttributeIdentity `json:"attributes"`
}

// ListSendersInput requests configured email senders.
type ListSendersInput struct{}

// SenderIdentity describes a configured sender without domain secrets.
type SenderIdentity struct {
	// ID is the Brevo sender identifier.
	ID int64 `json:"id"`
	// Name is the sender display name.
	Name string `json:"name"`
	// Email is the sender address.
	Email string `json:"email"`
	// Active indicates whether the sender is available for use.
	Active bool `json:"active"`
}

// SendersList contains configured sender identities.
type SendersList struct {
	// Senders is the current account sender collection.
	Senders []SenderIdentity `json:"senders"`
}

// ListContactsOperation retrieves a contact page.
type ListContactsOperation struct{ client *Client }

// UpdateContactOperation sets absolute contact attributes.
type UpdateContactOperation struct{ client *Client }

// DeleteContactOperation deletes a contact.
type DeleteContactOperation struct{ client *Client }

// ListAttributesOperation retrieves contact attribute definitions.
type ListAttributesOperation struct{ client *Client }

// ListSendersOperation retrieves configured email senders.
type ListSendersOperation struct{ client *Client }

// ListContacts binds contact listing to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// UpdateContact binds contact updates to this client.
func (client *Client) UpdateContact() UpdateContactOperation { return UpdateContactOperation{client} }

// DeleteContact binds contact deletion to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// ListAttributes binds attribute discovery to this client.
func (client *Client) ListAttributes() ListAttributesOperation {
	return ListAttributesOperation{client}
}

// ListSenders binds sender discovery to this client.
func (client *Client) ListSenders() ListSendersOperation { return ListSendersOperation{client} }

// Definition returns the generated contact listing contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Definition returns the generated contact update contract.
func (UpdateContactOperation) Definition() sdkgo.MutationDefinition { return UpdateContactDefinition }

// Definition returns the generated contact deletion contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// Definition returns the generated attribute listing contract.
func (ListAttributesOperation) Definition() sdkgo.QueryDefinition { return ListAttributesDefinition }

// Definition returns the generated sender listing contract.
func (ListSendersOperation) Definition() sdkgo.QueryDefinition { return ListSendersDefinition }

// IdempotencyKey preserves the call identity across absolute updates.
func (UpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves the call identity across deletions.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists a bounded page of contact IDs and emails.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsList] {
	if input.Limit < 0 || input.Limit > 1000 || input.Offset < 0 {
		return brevoQueryBranch(ListContactsBranchDefect, ContactsList{}, "listContacts", sdkgo.FailureValidation, "limit must be 1–1000 or zero, and offset must be non-negative")
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return invokeBrevoQuery(call, operation.client, "listContacts", "/contacts?"+query.Encode(), ListContactsBranchListed, ListContactsBranchProviderRejected, ListContactsBranchInvalidResponse, ListContactsBranchDefect, func(body []byte) (ContactsList, error) {
		var value ContactsList
		if err := json.Unmarshal(body, &value); err != nil || value.Contacts == nil || value.Count < 0 {
			return ContactsList{}, errors.New("invalid contacts page")
		}
		for _, contact := range value.Contacts {
			if contact.ID <= 0 || contact.Email != "" && !validEmail(contact.Email) {
				return ContactsList{}, errors.New("invalid contact identity")
			}
		}
		return value, nil
	})
}

// Invoke updates named contact attributes without changing list membership.
func (operation UpdateContactOperation) Invoke(call sdkgo.Call, input UpdateContactInput) sdkgo.MutationAttempt[ContactChangeResult] {
	const operationName = "updateContact"
	if !validEmail(input.Email) || len(input.Attributes) == 0 {
		return brevoMutationBranch(UpdateContactBranchDefect, ContactChangeResult{}, operationName, sdkgo.FailureValidation, "email and at least one attribute are required")
	}
	for name := range input.Attributes {
		if name == "" || strings.ToUpper(name) != name {
			return brevoMutationBranch(UpdateContactBranchDefect, ContactChangeResult{}, operationName, sdkgo.FailureValidation, "attribute names must be uppercase")
		}
	}
	payload, err := json.Marshal(struct {
		Attributes map[string]string `json:"attributes"`
	}{input.Attributes})
	if err != nil || len(payload) > 1<<20 {
		return brevoMutationBranch(UpdateContactBranchDefect, ContactChangeResult{}, operationName, sdkgo.FailureValidation, "contact update is invalid or too large")
	}
	return invokeBrevoNoContentMutation(call, operation.client, operationName, http.MethodPut, "/contacts/"+url.PathEscape(input.Email), payload, ContactChangeResult{Email: input.Email}, UpdateContactBranchUpdated, UpdateContactBranchNotFound, UpdateContactBranchProviderRejected, UpdateContactBranchDefect)
}

// Invoke deletes the contact identified by email.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[ContactChangeResult] {
	const operationName = "deleteContact"
	if !validEmail(input.Email) {
		return brevoMutationBranch(DeleteContactBranchDefect, ContactChangeResult{}, operationName, sdkgo.FailureValidation, "a plain email address is required")
	}
	return invokeBrevoNoContentMutation(call, operation.client, operationName, http.MethodDelete, "/contacts/"+url.PathEscape(input.Email), nil, ContactChangeResult{Email: input.Email}, DeleteContactBranchDeleted, DeleteContactBranchNotFound, DeleteContactBranchProviderRejected, DeleteContactBranchDefect)
}

// Invoke lists contact attribute definitions without contact data.
func (operation ListAttributesOperation) Invoke(call sdkgo.Call, _ ListAttributesInput) sdkgo.QueryAttempt[AttributesList] {
	return invokeBrevoQuery(call, operation.client, "listAttributes", "/contacts/attributes", ListAttributesBranchListed, ListAttributesBranchProviderRejected, ListAttributesBranchInvalidResponse, ListAttributesBranchDefect, func(body []byte) (AttributesList, error) {
		var value AttributesList
		if err := json.Unmarshal(body, &value); err != nil || value.Attributes == nil {
			return AttributesList{}, errors.New("invalid attributes list")
		}
		for _, attribute := range value.Attributes {
			if attribute.Name == "" || attribute.Category == "" || attribute.Type == "" {
				return AttributesList{}, errors.New("missing attribute identity")
			}
		}
		return value, nil
	})
}

// Invoke lists sender IDs and addresses without dedicated IP details.
func (operation ListSendersOperation) Invoke(call sdkgo.Call, _ ListSendersInput) sdkgo.QueryAttempt[SendersList] {
	return invokeBrevoQuery(call, operation.client, "listSenders", "/senders", ListSendersBranchListed, ListSendersBranchProviderRejected, ListSendersBranchInvalidResponse, ListSendersBranchDefect, func(body []byte) (SendersList, error) {
		var value SendersList
		if err := json.Unmarshal(body, &value); err != nil || value.Senders == nil {
			return SendersList{}, errors.New("invalid senders list")
		}
		for _, sender := range value.Senders {
			if sender.ID <= 0 || sender.Name == "" || !validEmail(sender.Email) {
				return SendersList{}, errors.New("missing sender identity")
			}
		}
		return value, nil
	})
}

func invokeBrevoQuery[T any](call sdkgo.Call, client *Client, operationName, path string, listed, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	request, failure := client.newRequest(call, http.MethodGet, path, nil, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(defect, zero, failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](*brevoFailure(operationName, sdkgo.FailureTransport, "Brevo response is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](*brevoFailure(operationName, sdkgo.FailureRateLimit, "Brevo rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if isRetryable(response.StatusCode) {
		return sdkgo.NewQueryRetry[T](*brevoFailure(operationName, sdkgo.FailureAvailability, "Brevo is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode != http.StatusOK {
		return brevoQueryBranch(rejected, zero, operationName, failureKind(response.StatusCode), "Brevo rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return brevoQueryBranch(invalid, zero, operationName, sdkgo.FailureResponseTooLarge, "Brevo response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](*brevoFailure(operationName, sdkgo.FailureTransport, "Brevo response was interrupted"), 0)
	}
	value, err := decode(body)
	if err != nil {
		return brevoQueryBranch(invalid, zero, operationName, sdkgo.FailureProtocol, "Brevo returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(listed, value, nil, sdkgo.Receipt{})
}

func invokeBrevoNoContentMutation[T any](call sdkgo.Call, client *Client, operationName, method, path string, payload []byte, value T, saved, notFound, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[T] {
	var zero T
	request, failure := client.newRequest(call, method, path, payload, operationName)
	if failure != nil {
		return sdkgo.NewMutationBranch(defect, zero, failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[T](*brevoFailure(operationName, sdkgo.FailureTransport, "Brevo response is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](*brevoFailure(operationName, sdkgo.FailureRateLimit, "Brevo rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if isRetryable(response.StatusCode) {
		return sdkgo.NewMutationRetry[T](*brevoFailure(operationName, sdkgo.FailureAvailability, "Brevo is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusNotFound {
		return brevoMutationBranch(notFound, zero, operationName, sdkgo.FailureNotFound, "Brevo did not find the contact")
	}
	if response.StatusCode != http.StatusNoContent {
		return brevoMutationBranch(rejected, zero, operationName, failureKind(response.StatusCode), "Brevo rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	return sdkgo.NewMutationBranch(saved, value, nil, sdkgo.Receipt{})
}

func brevoQueryBranch[T any](branch sdkgo.BranchID, value T, operationName string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryBranch(branch, value, brevoFailure(operationName, kind, message), sdkgo.Receipt{})
}

func brevoMutationBranch[T any](branch sdkgo.BranchID, value T, operationName string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationBranch(branch, value, brevoFailure(operationName, kind, message), sdkgo.Receipt{})
}
