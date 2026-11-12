// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var contactResourceName = regexp.MustCompile(`^people/[A-Za-z0-9_-]{1,128}$`)

// ListContactsInput selects one page of the connected user's contacts.
type ListContactsInput struct {
	// PageSize is 1 through 1000; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is Google's opaque cursor; blank selects the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// Contact projects a People API resource name and its first display name and email address.
type Contact struct {
	// ResourceName is a stable People API identity such as people/c123.
	ResourceName string `json:"resourceName"`
	// DisplayName is the first name returned with the personFields mask, if any.
	DisplayName string `json:"displayName"`
	// EmailAddress is the first email address returned with the personFields mask, if any.
	EmailAddress string `json:"emailAddress"`
}

// ContactsPage contains one bounded page and the next cursor.
type ContactsPage struct {
	// Contacts are projected contact identities.
	Contacts []Contact `json:"contacts"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// GetContactInput selects a contact by People API resource name.
type GetContactInput struct {
	// ResourceName is the people/{id} identity returned by listContacts.
	ResourceName string `json:"resourceName"`
}

// ListContactsOperation reads one contact page.
type ListContactsOperation struct{ client *Client }

// GetContactOperation reads one contact.
type GetContactOperation struct{ client *Client }

// ListContacts binds the contact page Query.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// GetContact binds the contact Query.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated contact page Query contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Definition returns the generated contact Query contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke lists contacts with a fixed field mask to limit returned personal data.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactsPage] {
	if input.PageSize < 0 || input.PageSize > 1000 || !validPageToken(input.PageToken) {
		return queryBranch("listContacts", "defect", ContactsPage{}, sdkgo.FailureValidation, "contact page input is invalid")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	query := url.Values{"personFields": {"names,emailAddresses"}, "pageSize": {strconv.Itoa(pageSize)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.request(call, "/v1/people/me/connections?"+query.Encode())
	if decision, failed := classifyRead[ContactsPage](response, "listContacts"); failed {
		return decision
	}
	var wire struct {
		Connections   []personWire `json:"connections"`
		NextPageToken string       `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || len(wire.Connections) > pageSize || !validPageToken(wire.NextPageToken) {
		return queryBranch("listContacts", "invalidResponse", ContactsPage{}, sdkgo.FailureProtocol, "Google returned invalid contact page JSON")
	}
	contacts := make([]Contact, 0, len(wire.Connections))
	for _, person := range wire.Connections {
		contact, ok := person.project()
		if !ok {
			return queryBranch("listContacts", "invalidResponse", ContactsPage{}, sdkgo.FailureProtocol, "Google returned an invalid contact resource")
		}
		contacts = append(contacts, contact)
	}
	return sdkgo.NewQueryBranch(ListContactsBranchListed, ContactsPage{Contacts: contacts, NextPageToken: wire.NextPageToken}, nil, sdkgo.Receipt{})
}

// Invoke gets one contact without retaining other People API fields.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[Contact] {
	if !contactResourceName.MatchString(input.ResourceName) {
		return queryBranch("getContact", "defect", Contact{}, sdkgo.FailureValidation, "contact resource name is invalid")
	}
	query := url.Values{"personFields": {"names,emailAddresses"}}
	response := operation.client.request(call, "/v1/"+input.ResourceName+"?"+query.Encode())
	if response.status == 404 && response.err == nil {
		return queryBranch("getContact", "notFound", Contact{}, sdkgo.FailureProviderRejection, "contact was not found")
	}
	if decision, failed := classifyRead[Contact](response, "getContact"); failed {
		return decision
	}
	var person personWire
	if err := json.Unmarshal(response.body, &person); err != nil {
		return queryBranch("getContact", "invalidResponse", Contact{}, sdkgo.FailureProtocol, "Google returned invalid contact JSON")
	}
	contact, ok := person.project()
	if !ok || contact.ResourceName != input.ResourceName {
		return queryBranch("getContact", "invalidResponse", Contact{}, sdkgo.FailureProtocol, "Google returned the wrong contact resource")
	}
	return sdkgo.NewQueryBranch(GetContactBranchFound, contact, nil, sdkgo.Receipt{})
}

type personWire struct {
	ResourceName string `json:"resourceName"`
	Names        []struct {
		DisplayName string `json:"displayName"`
	} `json:"names"`
	EmailAddresses []struct {
		Value string `json:"value"`
	} `json:"emailAddresses"`
}

func (person personWire) project() (Contact, bool) {
	if !contactResourceName.MatchString(person.ResourceName) {
		return Contact{}, false
	}
	contact := Contact{ResourceName: person.ResourceName}
	if len(person.Names) > 0 {
		contact.DisplayName = person.Names[0].DisplayName
	}
	if len(person.EmailAddresses) > 0 {
		contact.EmailAddress = person.EmailAddresses[0].Value
	}
	return contact, true
}

func classifyRead[T any](response providerResponse, operation string) (sdkgo.QueryAttempt[T], bool) {
	var zero T
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, "defect", zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return queryRetry[T](operation, sdkgo.FailureTransport, "Google Contacts request failed", 0), true
	}
	if response.status == 429 {
		return queryRetry[T](operation, sdkgo.FailureRateLimit, "Google rate limited the request", response.retryAfter), true
	}
	if response.status == 408 || response.status >= 500 {
		return queryRetry[T](operation, sdkgo.FailureAvailability, "Google Contacts is temporarily unavailable", response.retryAfter), true
	}
	if response.status != 200 {
		return queryBranch(operation, "providerRejected", zero, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, "invalidResponse", zero, sdkgo.FailureResponseTooLarge, "Google response exceeded the configured limit"), true
	}
	if response.err != nil {
		return queryRetry[T](operation, sdkgo.FailureTransport, "Google response was interrupted", 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func queryBranch[T any](operation, branch string, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(sdkgo.BranchID(branch), value, &failure, sdkgo.Receipt{})
}

func queryRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-contacts", Operation: operation, Message: message}
	return sdkgo.NewQueryRetry[T](failure, delay)
}

func validPageToken(value string) bool {
	return len(value) <= 4096 && !strings.ContainsAny(value, "\r\n")
}
