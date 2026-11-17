// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetContactInput identifies one company contact.
type GetContactInput struct {
	ContactID string `json:"contactId"`
}

// ListContactsInput selects a bounded page of company contacts.
type ListContactsInput struct {
	PageSize  int    `json:"pageSize,omitempty"`
	PageToken string `json:"pageToken,omitempty"`
}

// ContactsPage contains one company contact page and an opaque continuation token.
type ContactsPage struct {
	Contacts      []Contact `json:"contacts"`
	NextPageToken string    `json:"nextPageToken,omitempty"`
}

// GetContactOperation reads a contact by ID.
type GetContactOperation struct{ client *Client }

// ListContactsOperation lists company contacts.
type ListContactsOperation struct{ client *Client }

// GetContact binds the query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// ListContacts binds the query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// Definition returns the manifest-generated operation contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Definition returns the manifest-generated operation contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Invoke reads the requested Front contact.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[Contact] {
	operationID := GetContactDefinition.Operation.OperationID
	if !contactIDPattern.MatchString(input.ContactID) {
		return sdkgo.NewQueryBranch(GetContactBranchDefect, Contact{}, frontFailurePointer(sdkgo.FailureValidation, operationID, "contactId must be a Front crd_ ID"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetContactBranchDefect, Contact{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: "/contacts/" + input.ContactID})
	receipt := operation.client.receipt(call, input.ContactID)
	switch {
	case result.isRetryable():
		return sdkgo.NewQueryRetry[Contact](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(GetContactBranchNotFound, Contact{}, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(GetContactBranchDefect, Contact{}, &result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(GetContactBranchInvalidResponse, Contact{}, &result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return sdkgo.NewQueryBranch(GetContactBranchProviderRejected, Contact{}, &result.failure, receipt)
	}
	contact, err := decodeContact(result.response.body, "")
	if err != nil || contact.ID != input.ContactID {
		return sdkgo.NewQueryBranch(GetContactBranchInvalidResponse, Contact{}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned a contact with an invalid identity"), receipt)
	}
	return sdkgo.NewQueryBranch(GetContactBranchFound, contact, nil, receipt)
}

// Invoke lists one bounded contact page.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	operationID := ListContactsDefinition.Operation.OperationID
	output := ContactsPage{Contacts: []Contact{}}
	if input.PageSize < 0 || input.PageSize > 100 || (input.PageToken != "" && !pageTokenPattern.MatchString(input.PageToken)) {
		return sdkgo.NewQueryBranch(ListContactsBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, "pageSize or pageToken is invalid"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListContactsBranchDefect, output, failure, sdkgo.Receipt{})
	}
	parameters := url.Values{"limit": {strconv.Itoa(pageSizeOrDefault(input.PageSize))}}
	if input.PageToken != "" {
		parameters.Set("page_token", input.PageToken)
	}
	result := operation.client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: "/contacts", query: parameters})
	receipt := operation.client.receipt(call, "")
	switch {
	case result.isRetryable():
		return sdkgo.NewQueryRetry[ContactsPage](result.failure, result.retryAfter)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(ListContactsBranchDefect, output, &result.failure, sdkgo.Receipt{})
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(ListContactsBranchInvalidResponse, output, &result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return sdkgo.NewQueryBranch(ListContactsBranchProviderRejected, output, &result.failure, receipt)
	}
	var page frontPageWire[json.RawMessage]
	if json.Unmarshal(result.response.body, &page) != nil || page.Results == nil {
		return sdkgo.NewQueryBranch(ListContactsBranchInvalidResponse, output, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid contact page"), receipt)
	}
	for _, item := range page.Results {
		contact, err := decodeContact(item, "")
		if err != nil {
			return sdkgo.NewQueryBranch(ListContactsBranchInvalidResponse, ContactsPage{Contacts: []Contact{}}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid contact"), receipt)
		}
		output.Contacts = append(output.Contacts, contact)
	}
	var err error
	output.NextPageToken, err = operation.client.nextPageToken(page.Pagination.Next, "/contacts")
	if err != nil {
		return sdkgo.NewQueryBranch(ListContactsBranchInvalidResponse, ContactsPage{Contacts: []Contact{}}, frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid next-page link"), receipt)
	}
	return sdkgo.NewQueryBranch(ListContactsBranchListed, output, nil, receipt)
}

func pageSizeOrDefault(pageSize int) int {
	if pageSize == 0 {
		return 25
	}
	return pageSize
}
