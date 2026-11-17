// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendgrid

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Contact is the stable subset of a SendGrid marketing contact.
type Contact struct {
	// ID is the provider-issued contact ID.
	ID string `json:"id"`
	// Email is the contact's primary address, when present.
	Email string `json:"email,omitempty"`
	// FirstName is the contact's given name.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the contact's family name.
	LastName string `json:"lastName,omitempty"`
}

// ContactIDInput identifies one marketing contact.
type ContactIDInput struct {
	// ID is the provider-issued contact ID from SendGrid Marketing > Contacts.
	ID string `json:"id"`
}

// GetContactOperation reads one marketing contact by ID.
type GetContactOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// GetContact binds the operation to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Invoke reads a contact through the Marketing Contacts API.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input ContactIDInput) sdkgo.QueryAttempt[Contact] {
	branches := queryBranches{found: GetContactBranchFound, notFound: GetContactBranchNotFound, rejected: GetContactBranchProviderRejected, invalid: GetContactBranchInvalidResponse, defect: GetContactBranchDefect}
	if !isSafeIdentifier(input.ID) {
		return queryBranch("getContact", branches.defect, Contact{}, sdkgo.FailureValidation, "contact ID is invalid")
	}
	return invokeJSONQuery(call, operation.client, "getContact", branches, http.MethodGet, "/v3/marketing/contacts/"+url.PathEscape(input.ID), nil, nil, decodeContact)
}

// SearchContactsInput contains one SGQL search expression.
type SearchContactsInput struct {
	// Query is a SendGrid Query Language expression, such as email LIKE 'a%'.
	Query string `json:"query"`
}

// SearchContactsOutput contains the provider's bounded result set.
type SearchContactsOutput struct {
	// Contacts contains at most the first 50 matching contacts.
	Contacts []Contact `json:"contacts"`
	// Count is the provider-reported matching count.
	Count int `json:"count"`
}

// SearchContactsOperation searches marketing contacts.
type SearchContactsOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (SearchContactsOperation) Definition() sdkgo.QueryDefinition { return SearchContactsDefinition }

// SearchContacts binds the operation to this client.
func (client *Client) SearchContacts() SearchContactsOperation {
	return SearchContactsOperation{client}
}

// Invoke searches contacts with a caller-supplied SGQL expression.
func (operation SearchContactsOperation) Invoke(call sdkgo.Call, input SearchContactsInput) sdkgo.QueryAttempt[SearchContactsOutput] {
	branches := queryBranches{found: SearchContactsBranchFound, rejected: SearchContactsBranchProviderRejected, invalid: SearchContactsBranchInvalidResponse, defect: SearchContactsBranchDefect}
	if strings.TrimSpace(input.Query) == "" || len(input.Query) > 4096 {
		return queryBranch("searchContacts", branches.defect, SearchContactsOutput{}, sdkgo.FailureValidation, "query must be a bounded SGQL expression")
	}
	payload, _ := json.Marshal(map[string]string{"query": input.Query})
	return invokeJSONQuery(call, operation.client, "searchContacts", branches, http.MethodPost, "/v3/marketing/contacts/search", nil, payload, decodeContactSearch)
}

// UpsertContactInput supplies a contact's email and changed name fields.
type UpsertContactInput struct {
	// Email is the contact's primary address and stable upsert identity.
	Email string `json:"email"`
	// FirstName sets the given name; blank omits it.
	FirstName string `json:"firstName,omitempty"`
	// LastName sets the family name; blank omits it.
	LastName string `json:"lastName,omitempty"`
}

// ContactJobOutput identifies an asynchronous marketing job.
type ContactJobOutput struct {
	// JobID is the SendGrid job ID; acceptance does not mean processing finished.
	JobID string `json:"jobId"`
}

// UpsertContactOperation queues one contact upsert by email.
type UpsertContactOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (UpsertContactOperation) Definition() sdkgo.MutationDefinition { return UpsertContactDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (UpsertContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// UpsertContact binds the operation to this client.
func (client *Client) UpsertContact() UpsertContactOperation { return UpsertContactOperation{client} }

// Invoke queues an absolute replacement of the supplied email and name values.
func (operation UpsertContactOperation) Invoke(call sdkgo.Call, input UpsertContactInput) sdkgo.MutationAttempt[ContactJobOutput] {
	branches := mutationBranches{completed: UpsertContactBranchQueued, rejected: UpsertContactBranchProviderRejected, invalid: UpsertContactBranchInvalidResponse, defect: UpsertContactBranchDefect}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 254 || len(input.FirstName) > 50 || len(input.LastName) > 50 {
		return mutationBranch("upsertContact", branches.defect, ContactJobOutput{}, sdkgo.FailureValidation, "email or contact name is invalid")
	}
	contact := map[string]string{"email": strings.ToLower(input.Email)}
	if input.FirstName != "" {
		contact["first_name"] = input.FirstName
	}
	if input.LastName != "" {
		contact["last_name"] = input.LastName
	}
	payload, _ := json.Marshal(map[string]any{"contacts": []any{contact}})
	return invokeSafeMutation(call, operation.client, "upsertContact", branches, http.MethodPut, "/v3/marketing/contacts", payload, http.StatusAccepted, decodeContactJob)
}

// DeleteContactOperation queues deletion of one contact by ID.
type DeleteContactOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ ContactIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteContact binds the operation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Invoke queues deletion of exactly the named contact.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input ContactIDInput) sdkgo.MutationAttempt[ContactJobOutput] {
	branches := mutationBranches{completed: DeleteContactBranchQueued, rejected: DeleteContactBranchProviderRejected, invalid: DeleteContactBranchInvalidResponse, defect: DeleteContactBranchDefect}
	if !isSafeIdentifier(input.ID) {
		return mutationBranch("deleteContact", branches.defect, ContactJobOutput{}, sdkgo.FailureValidation, "contact ID is invalid")
	}
	query := url.Values{"ids": {input.ID}}
	body, status, retryAfter, err := operation.client.do(call, http.MethodDelete, "/v3/marketing/contacts", query, nil)
	if status == http.StatusAccepted && err == nil {
		output, decodeErr := decodeContactJob(body)
		if decodeErr == nil {
			return sdkgo.NewMutationBranch(DeleteContactBranchQueued, output, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "sendgrid"})
		}
		return mutationBranch("deleteContact", branches.invalid, ContactJobOutput{}, sdkgo.FailureProtocol, "provider returned an invalid deletion job")
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable || status >= 500 || errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[ContactJobOutput](queryFailure("deleteContact", sdkgo.FailureAvailability, "provider deletion outcome is unknown"), retryAfter)
	}
	if errors.Is(err, errCredential) {
		return mutationBranch("deleteContact", branches.defect, ContactJobOutput{}, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	return mutationBranch("deleteContact", branches.rejected, ContactJobOutput{}, sdkgo.FailureProviderRejection, "provider rejected contact deletion")
}

func isSafeIdentifier(identifier string) bool {
	return identifier != "" && len(identifier) <= 128 && !strings.ContainsAny(identifier, "/?#\r\n")
}

func decodeContact(body []byte) (Contact, error) {
	var wire struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID == "" {
		return Contact{}, errors.New("contact response is incomplete")
	}
	return Contact{ID: wire.ID, Email: wire.Email, FirstName: wire.FirstName, LastName: wire.LastName}, nil
}

func decodeContactSearch(body []byte) (SearchContactsOutput, error) {
	var wire struct {
		Result       []json.RawMessage `json:"result"`
		ContactCount int               `json:"contact_count"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Result == nil {
		return SearchContactsOutput{}, errors.New("contact search result is missing")
	}
	output := SearchContactsOutput{Contacts: make([]Contact, 0, len(wire.Result)), Count: wire.ContactCount}
	for _, rawContact := range wire.Result {
		contact, err := decodeContact(rawContact)
		if err != nil {
			return SearchContactsOutput{}, err
		}
		output.Contacts = append(output.Contacts, contact)
	}
	return output, nil
}

func decodeContactJob(body []byte) (ContactJobOutput, error) {
	var wire struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.JobID == "" {
		return ContactJobOutput{}, errors.New("contact job ID is missing")
	}
	return ContactJobOutput{JobID: wire.JobID}, nil
}
