// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListContactsInput selects one page of contacts, optionally by exact email.
type ListContactsInput struct {
	// EmailAddress matches an exact email address; blank returns all active contacts.
	EmailAddress string `json:"emailAddress,omitempty"`
	// Limit bounds one page to 1 through 500 contacts; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// Cursor is the opaque cursor returned by the preceding page; blank starts a search.
	Cursor string `json:"cursor,omitempty"`
}

// ContactsPage contains selected contact fields and the next page cursor.
type ContactsPage struct {
	// Contacts holds one page of contact summaries.
	Contacts []ContactMetadata `json:"contacts"`
	// NextCursor is blank when there are no more contacts.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListContactsOperation searches the contact collection.
type ListContactsOperation struct{ client *Client }

// ListContacts binds the Query to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// Definition returns the generated Query contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Invoke reads a page and classifies provider outcomes.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	if input.Limit < 0 || input.Limit > 500 || len(input.Cursor) > 4096 || strings.ContainsAny(input.Cursor, "\r\n") || len(input.EmailAddress) > 254 || strings.ContainsAny(input.EmailAddress, "\r\n") {
		return listContactsBranch(ListContactsBranchDefect, sdkgo.FailureValidation, "invalid contact search input")
	}
	parameters := url.Values{}
	if input.EmailAddress != "" {
		parameters.Set("email", input.EmailAddress)
	}
	if input.Limit > 0 {
		parameters.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Cursor != "" {
		parameters.Set("cursor", input.Cursor)
	}
	path := "/contacts"
	if len(parameters) > 0 {
		path += "?" + parameters.Encode()
	}
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return listContactsBranch(ListContactsBranchDefect, sdkgo.FailureAuthentication, "Constant Contact credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) {
		return sdkgo.NewQueryRetry[ContactsPage](listContactsFailure(sdkgo.FailureAvailability, "Constant Contact credentials could not be refreshed"), 0)
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ContactsPage](listContactsFailure(sdkgo.FailureTransport, "Constant Contact request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ContactsPage](listContactsFailure(sdkgo.FailureRateLimit, "Constant Contact rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ContactsPage](listContactsFailure(sdkgo.FailureAvailability, "Constant Contact is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusAccepted {
		return sdkgo.NewQueryRetry[ContactsPage](listContactsFailure(sdkgo.FailureAvailability, "Constant Contact is preparing the contact search"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listContactsBranch(ListContactsBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listContactsBranch(ListContactsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Constant Contact response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ContactsPage](listContactsFailure(sdkgo.FailureTransport, "Constant Contact response was interrupted"), 0)
	}
	var envelope struct {
		Contacts []json.RawMessage `json:"contacts"`
		Links    struct {
			Next struct {
				Href string `json:"href"`
			} `json:"next"`
		} `json:"_links"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil || envelope.Contacts == nil {
		return listContactsBranch(ListContactsBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned invalid contact JSON")
	}
	page := ContactsPage{Contacts: make([]ContactMetadata, 0, len(envelope.Contacts))}
	for _, item := range envelope.Contacts {
		contact, err := decodeContact(item)
		if err != nil {
			return listContactsBranch(ListContactsBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid contact")
		}
		page.Contacts = append(page.Contacts, contact)
	}
	if envelope.Links.Next.Href != "" {
		nextURL, err := url.Parse(envelope.Links.Next.Href)
		if err != nil || nextURL.Host != "" || !strings.HasSuffix(nextURL.Path, "/contacts") || nextURL.Query().Get("cursor") == "" {
			return listContactsBranch(ListContactsBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid next page link")
		}
		page.NextCursor = nextURL.Query().Get("cursor")
	}
	return sdkgo.NewQueryBranch(ListContactsBranchListed, page, nil, sdkgo.Receipt{})
}

func listContactsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "constantcontact", Operation: "listContacts", Message: message}
}

func listContactsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ContactsPage] {
	failure := listContactsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ContactsPage{}, &failure, sdkgo.Receipt{})
}
