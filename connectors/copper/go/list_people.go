// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListPeopleInput selects one Copper search page with optional filters.
type ListPeopleInput struct {
	// PageNumber starts at one; zero uses Copper's first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero uses Copper's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// Name optionally matches a full person name.
	Name string `json:"name,omitempty"`
	// Email optionally matches a work email address.
	Email string `json:"email,omitempty"`
}

// PeoplePage contains one returned page of person summaries.
type PeoplePage struct {
	// People are the records returned by Copper for this page.
	People []PersonMetadata `json:"people"`
}

// ListPeopleOperation searches Copper people without changing them.
type ListPeopleOperation struct{ client *Client }

// ListPeople binds the Query to this client.
func (client *Client) ListPeople() ListPeopleOperation { return ListPeopleOperation{client} }

// Definition returns the generated Query contract.
func (ListPeopleOperation) Definition() sdkgo.QueryDefinition { return ListPeopleDefinition }

// Invoke reads one page through Copper's people search endpoint.
func (operation ListPeopleOperation) Invoke(call sdkgo.Call, input ListPeopleInput) sdkgo.QueryAttempt[PeoplePage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 ||
		len(input.Name) > 255 || len(input.Email) > 254 || strings.ContainsAny(input.Name+input.Email, "\r\n") {
		return listPeopleBranch(ListPeopleBranchDefect, sdkgo.FailureValidation, "invalid people page or filter")
	}
	request := struct {
		PageNumber int      `json:"page_number,omitempty"`
		PageSize   int      `json:"page_size,omitempty"`
		Name       string   `json:"name,omitempty"`
		Emails     []string `json:"emails,omitempty"`
	}{PageNumber: input.PageNumber, PageSize: input.PageSize, Name: input.Name}
	if input.Email != "" {
		request.Emails = []string{input.Email}
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return listPeopleBranch(ListPeopleBranchDefect, sdkgo.FailureLocalDefect, "people search could not be encoded")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodPost, "/people/search", requestBody)
	if errors.Is(response.err, errCredential) {
		return listPeopleBranch(ListPeopleBranchDefect, sdkgo.FailureAuthentication, "Copper credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PeoplePage](queryFailure("listPeople", sdkgo.FailureRateLimit, "Copper rate limited people search"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PeoplePage](queryFailure("listPeople", sdkgo.FailureAvailability, "Copper people search is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listPeopleBranch(ListPeopleBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Copper people response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PeoplePage](queryFailure("listPeople", sdkgo.FailureTransport, "Copper people response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return listPeopleBranch(ListPeopleBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return listPeopleBranch(ListPeopleBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned invalid people JSON")
	}
	page := PeoplePage{People: make([]PersonMetadata, 0, len(records))}
	for _, record := range records {
		person, decodeError := decodePerson(record)
		if decodeError != nil {
			return listPeopleBranch(ListPeopleBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned an invalid person")
		}
		page.People = append(page.People, person)
	}
	return sdkgo.NewQueryBranch(ListPeopleBranchListed, page, nil, sdkgo.Receipt{})
}

func listPeopleBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PeoplePage] {
	failure := queryFailure("listPeople", kind, message)
	return sdkgo.NewQueryBranch(branch, PeoplePage{}, &failure, sdkgo.Receipt{})
}
