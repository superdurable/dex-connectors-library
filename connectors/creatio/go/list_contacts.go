// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContactsInput selects one bounded page of Creatio contacts.
type ListContactsInput struct {
	// PageNumber starts at one; zero selects the first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero selects 20 records.
	PageSize int `json:"pageSize,omitempty"`
}

// ContactsPage contains one Creatio OData result page.
type ContactsPage struct {
	// Contacts are the selected public contact fields.
	Contacts []ContactSummary `json:"contacts"`
}

// ListContactsOperation reads one OData page without changing records.
type ListContactsOperation struct{ client *Client }

// ListContacts binds the Query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// Definition returns the generated Query contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Invoke reads contact IDs, names, and emails from one OData page.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 {
		return contactsPageBranch(ListContactsBranchDefect, sdkgo.FailureValidation, "invalid contact page")
	}
	pageNumber, pageSize := input.PageNumber, input.PageSize
	if pageNumber == 0 {
		pageNumber = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return contactsPageBranch(ListContactsBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[ContactsPage](contactsFailure("listContacts", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	query := url.Values{"$select": {"Id,Name,Email"}, "$top": {strconv.Itoa(pageSize)}, "$skip": {strconv.Itoa((pageNumber - 1) * pageSize)}, "$orderby": {"Id"}}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Contact?"+query.Encode(), nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ContactsPage](contactsFailure("listContacts", sdkgo.FailureRateLimit, "Creatio rate limited contacts"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ContactsPage](contactsFailure("listContacts", sdkgo.FailureAvailability, "Creatio contacts are unavailable"), response.retryAfter)
	}
	if response.isBodyTooLarge {
		return contactsPageBranch(ListContactsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio contacts response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ContactsPage](contactsFailure("listContacts", sdkgo.FailureTransport, "Creatio contacts response failed"), 0)
	}
	if response.status != http.StatusOK {
		return contactsPageBranch(ListContactsBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected contacts HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return contactsPageBranch(ListContactsBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid contacts JSON")
	}
	page := ContactsPage{Contacts: make([]ContactSummary, 0, len(envelope.Value))}
	for _, record := range envelope.Value {
		contact, err := decodeContactRecord(record)
		if err != nil {
			return contactsPageBranch(ListContactsBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned an invalid contact")
		}
		page.Contacts = append(page.Contacts, contact)
	}
	return sdkgo.NewQueryBranch(ListContactsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeContactRecord(body []byte) (ContactSummary, error) {
	var wire struct {
		ID    string  `json:"Id"`
		Name  string  `json:"Name"`
		Email *string `json:"Email"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactSummary{}, err
	}
	if !contactUUIDPattern.MatchString(wire.ID) || strings.TrimSpace(wire.Name) == "" {
		return ContactSummary{}, errors.New("contact identity missing")
	}
	contact := ContactSummary{ID: wire.ID, Name: wire.Name}
	if wire.Email != nil {
		contact.Email = *wire.Email
	}
	return contact, nil
}

func contactsFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "creatio", Operation: operation, Message: message}
}

func contactsPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ContactsPage] {
	failure := contactsFailure("listContacts", kind, message)
	return sdkgo.NewQueryBranch(branch, ContactsPage{}, &failure, sdkgo.Receipt{})
}
