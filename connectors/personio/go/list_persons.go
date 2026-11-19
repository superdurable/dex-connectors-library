// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personio

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListPersonsInput selects a page and optional Personio filters.
type ListPersonsInput struct {
	// Limit is 1 to 50 persons; zero uses Personio's default page size.
	Limit int `json:"limit,omitempty"`
	// Cursor is the opaque cursor from a previous page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// FirstName filters exact first names; blank disables the filter.
	FirstName string `json:"firstName,omitempty"`
	// LastName filters exact last names; blank disables the filter.
	LastName string `json:"lastName,omitempty"`
	// Status is ACTIVE or INACTIVE; blank includes both.
	Status string `json:"status,omitempty"`
}

// PersonPage contains selected person fields and the next page cursor.
type PersonPage struct {
	// Persons contains non-secret ID and name fields only.
	Persons []PersonProfile `json:"persons"`
	// NextCursor is blank when no following page is advertised.
	NextCursor string `json:"nextCursor"`
}

// ListPersonsOperation reads one page of Personio persons.
type ListPersonsOperation struct{ client *Client }

// ListPersons binds the Query to this client.
func (client *Client) ListPersons() ListPersonsOperation { return ListPersonsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListPersonsOperation) Definition() sdkgo.QueryDefinition { return ListPersonsDefinition }

// Invoke reads one page without returning email or other personnel fields.
func (operation ListPersonsOperation) Invoke(call sdkgo.Call, input ListPersonsInput) sdkgo.QueryAttempt[PersonPage] {
	if input.Limit < 0 || input.Limit > 50 || (input.Status != "" && input.Status != "ACTIVE" && input.Status != "INACTIVE") || len(input.Cursor) > 1024 {
		return personPageBranch(ListPersonsBranchDefect, sdkgo.FailureValidation, "Personio list filters are invalid")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return personPageBranch(ListPersonsBranchDefect, sdkgo.FailureAuthentication, "Personio connection needs authorization")
		}
		return personPageRetry(sdkgo.FailureTransport, "Personio credential refresh failed", 0)
	}
	token := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(token) {
		return personPageBranch(ListPersonsBranchDefect, sdkgo.FailureAuthentication, "Personio access token is unavailable")
	}
	query := url.Values{}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	if input.FirstName != "" {
		query.Set("first_name", input.FirstName)
	}
	if input.LastName != "" {
		query.Set("last_name", input.LastName)
	}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	endpoint := operation.client.baseURL + "/persons"
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return personPageBranch(ListPersonsBranchDefect, sdkgo.FailureValidation, "Personio request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return personPageRetry(sdkgo.FailureTransport, "Personio request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if response.StatusCode == http.StatusTooManyRequests {
		return personPageRetry(sdkgo.FailureRateLimit, "Personio rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return personPageRetry(sdkgo.FailureAvailability, "Personio is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return personPageBranch(ListPersonsBranchProviderRejected, sdkgo.FailureProviderRejection, "Personio rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return personPageBranch(ListPersonsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Personio response exceeded the configured limit")
	}
	if readErr != nil {
		return personPageRetry(sdkgo.FailureTransport, "Personio response was interrupted", 0)
	}
	var record struct {
		Data []struct {
			ID        string `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"_data"`
		Meta struct {
			Links struct {
				Next struct {
					Href string `json:"href"`
				} `json:"next"`
			} `json:"links"`
		} `json:"_meta"`
	}
	if json.Unmarshal(body, &record) != nil || record.Data == nil {
		return personPageBranch(ListPersonsBranchInvalidResponse, sdkgo.FailureProtocol, "Personio returned invalid persons JSON")
	}
	page := PersonPage{Persons: make([]PersonProfile, 0, len(record.Data))}
	for _, person := range record.Data {
		if !personIDPattern.MatchString(person.ID) {
			return personPageBranch(ListPersonsBranchInvalidResponse, sdkgo.FailureProtocol, "Personio returned an invalid person ID")
		}
		page.Persons = append(page.Persons, PersonProfile{ID: person.ID, FirstName: person.FirstName, LastName: person.LastName})
	}
	if record.Meta.Links.Next.Href != "" {
		next, err := url.Parse(record.Meta.Links.Next.Href)
		base, baseError := url.Parse(operation.client.baseURL)
		if err != nil || baseError != nil || next.Path != "/v2/persons" || (next.IsAbs() && (next.Host != base.Host || next.Scheme != base.Scheme)) || next.Query().Get("cursor") == "" {
			return personPageBranch(ListPersonsBranchInvalidResponse, sdkgo.FailureProtocol, "Personio returned an invalid next-page link")
		}
		page.NextCursor = next.Query().Get("cursor")
	}
	return sdkgo.NewQueryBranch(ListPersonsBranchListed, page, nil, sdkgo.Receipt{})
}

func personPageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "personio", Operation: "listPersons", Message: message}
}
func personPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PersonPage] {
	failure := personPageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PersonPage{}, &failure, sdkgo.Receipt{})
}
func personPageRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[PersonPage] {
	return sdkgo.NewQueryRetry[PersonPage](personPageFailure(kind, message), delay)
}
