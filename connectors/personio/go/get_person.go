// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personio

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var personIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// GetPersonInput selects one Personio person by its stable API ID.
type GetPersonInput struct {
	// PersonID is the ID returned by the Personio v2 Persons API.
	PersonID string `json:"personId"`
}

// PersonProfile contains selected names and an ID, excluding email and other personnel data.
type PersonProfile struct {
	// ID is the stable Personio person ID.
	ID string `json:"id"`
	// FirstName is the person's first name, or empty when absent.
	FirstName string `json:"firstName"`
	// LastName is the person's last name, or empty when absent.
	LastName string `json:"lastName"`
}

// GetPersonOperation reads one Personio person.
type GetPersonOperation struct{ client *Client }

// GetPerson binds the Query to this client.
func (client *Client) GetPerson() GetPersonOperation { return GetPersonOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetPersonOperation) Definition() sdkgo.QueryDefinition { return GetPersonDefinition }

// Invoke classifies Personio transport, HTTP, and response outcomes.
func (operation GetPersonOperation) Invoke(call sdkgo.Call, input GetPersonInput) sdkgo.QueryAttempt[PersonProfile] {
	if !personIDPattern.MatchString(input.PersonID) {
		return personBranch(GetPersonBranchDefect, sdkgo.FailureValidation, "Personio person ID must be an API ID")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return personBranch(GetPersonBranchDefect, sdkgo.FailureAuthentication, "Personio connection needs authorization")
		}
		return personRetry(sdkgo.FailureTransport, "Personio credential refresh failed", 0)
	}
	token := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(token) {
		return personBranch(GetPersonBranchDefect, sdkgo.FailureAuthentication, "Personio access token is unavailable")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/persons/"+input.PersonID, nil)
	if err != nil {
		return personBranch(GetPersonBranchDefect, sdkgo.FailureValidation, "Personio request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return personRetry(sdkgo.FailureTransport, "Personio request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if response.StatusCode == http.StatusTooManyRequests {
		return personRetry(sdkgo.FailureRateLimit, "Personio rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return personRetry(sdkgo.FailureAvailability, "Personio is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return personBranch(GetPersonBranchNotFound, sdkgo.FailureNotFound, "Personio person was not found")
	}
	if response.StatusCode != http.StatusOK {
		return personBranch(GetPersonBranchProviderRejected, sdkgo.FailureProviderRejection, "Personio rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return personBranch(GetPersonBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Personio response exceeded the configured limit")
	}
	if readErr != nil {
		return personRetry(sdkgo.FailureTransport, "Personio response was interrupted", 0)
	}
	var record struct {
		ID        string  `json:"id"`
		FirstName *string `json:"first_name"`
		LastName  *string `json:"last_name"`
	}
	if err := json.Unmarshal(body, &record); err != nil || record.ID != input.PersonID {
		return personBranch(GetPersonBranchInvalidResponse, sdkgo.FailureProtocol, "Personio returned invalid person JSON")
	}
	profile := PersonProfile{ID: record.ID}
	if record.FirstName != nil {
		profile.FirstName = *record.FirstName
	}
	if record.LastName != nil {
		profile.LastName = *record.LastName
	}
	return sdkgo.NewQueryBranch(GetPersonBranchFound, profile, nil, sdkgo.Receipt{})
}

func personFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "personio", Operation: "getPerson", Message: message}
}
func personBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PersonProfile] {
	failure := personFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PersonProfile{}, &failure, sdkgo.Receipt{})
}
func personRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[PersonProfile] {
	return sdkgo.NewQueryRetry[PersonProfile](personFailure(kind, message), delay)
}
