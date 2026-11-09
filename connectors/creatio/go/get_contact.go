// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var contactUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// GetContactInput identifies one Creatio contact by UUID.
type GetContactInput struct {
	// ContactID is the Contact.Id value from the Creatio Contacts page or OData.
	ContactID string `json:"contactId"`
}

// ContactSummary contains selected Creatio contact fields.
type ContactSummary struct {
	// ID is the stable Contact.Id UUID.
	ID string `json:"id"`
	// Name is the contact's displayed full name.
	Name string `json:"name"`
	// Email is the contact email, or blank when absent.
	Email string `json:"email"`
}

// GetContactOperation reads one contact through OData 4.
type GetContactOperation struct{ client *Client }

// GetContact binds the Query to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// Invoke classifies Creatio token, transport, HTTP, and OData outcomes.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[ContactSummary] {
	if !contactUUIDPattern.MatchString(input.ContactID) {
		return contactBranch(GetContactBranchDefect, sdkgo.FailureValidation, "Creatio contact UUID is invalid")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return contactBranch(GetContactBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return contactRetry(sdkgo.FailureTransport, "Creatio credential minting failed", 0)
	}
	token := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(token) {
		return contactBranch(GetContactBranchDefect, sdkgo.FailureAuthentication, "Creatio access token is unavailable")
	}
	query := url.Values{"$filter": {"Id eq " + input.ContactID}, "$select": {"Id,Name,Email"}, "$top": {"1"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/0/odata/Contact?"+query.Encode(), nil)
	if err != nil {
		return contactBranch(GetContactBranchDefect, sdkgo.FailureValidation, "Creatio request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return contactRetry(sdkgo.FailureTransport, "Creatio request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if bytes.Contains(body, []byte(token)) {
		return contactBranch(GetContactBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return contactRetry(sdkgo.FailureRateLimit, "Creatio rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return contactRetry(sdkgo.FailureAvailability, "Creatio is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return contactBranch(GetContactBranchNotFound, sdkgo.FailureNotFound, "Creatio contact was not found")
	}
	if response.StatusCode != http.StatusOK {
		return contactBranch(GetContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return contactBranch(GetContactBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio response exceeded the configured limit")
	}
	if readErr != nil {
		return contactRetry(sdkgo.FailureTransport, "Creatio response was interrupted", 0)
	}
	contact, found, err := decodeContact(body, input.ContactID)
	if err != nil {
		return contactBranch(GetContactBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid contact JSON")
	}
	if !found {
		return contactBranch(GetContactBranchNotFound, sdkgo.FailureNotFound, "Creatio contact was not found")
	}
	return sdkgo.NewQueryBranch(GetContactBranchFound, contact, nil, sdkgo.Receipt{})
}

func decodeContact(body []byte, requestedID string) (ContactSummary, bool, error) {
	var envelope struct {
		Value []struct {
			ID    string  `json:"Id"`
			Name  string  `json:"Name"`
			Email *string `json:"Email"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ContactSummary{}, false, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		return ContactSummary{}, false, err
	}
	if _, hasValue := document["value"]; !hasValue || envelope.Value == nil {
		return ContactSummary{}, false, errors.New("Creatio OData value array is missing")
	}
	if len(envelope.Value) == 0 {
		return ContactSummary{}, false, nil
	}
	if len(envelope.Value) != 1 {
		return ContactSummary{}, false, errors.New("Creatio returned multiple contacts for one UUID")
	}
	record := envelope.Value[0]
	if !contactUUIDPattern.MatchString(record.ID) || !strings.EqualFold(record.ID, requestedID) || strings.TrimSpace(record.Name) == "" {
		return ContactSummary{}, false, errors.New("Creatio contact fields are invalid")
	}
	email := ""
	if record.Email != nil {
		email = *record.Email
	}
	return ContactSummary{ID: record.ID, Name: record.Name, Email: email}, true, nil
}

func contactFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "creatio", Operation: "getContact", Message: message}
}
func contactBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ContactSummary] {
	failure := contactFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ContactSummary{}, &failure, sdkgo.Receipt{})
}
func contactRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ContactSummary] {
	return sdkgo.NewQueryRetry[ContactSummary](contactFailure(kind, message), delay)
}
