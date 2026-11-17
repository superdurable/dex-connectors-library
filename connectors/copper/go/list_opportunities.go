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

// ListOpportunitiesInput selects one Copper search page with optional filters.
type ListOpportunitiesInput struct {
	// PageNumber starts at one; zero uses Copper's first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero uses Copper's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// Name optionally matches a full opportunity name.
	Name string `json:"name,omitempty"`
}

// OpportunitiesPage contains one returned page of opportunity summaries.
type OpportunitiesPage struct {
	// Opportunities are the records returned by Copper for this page.
	Opportunities []OpportunityMetadata `json:"opportunities"`
}

// ListOpportunitiesOperation searches Copper opportunities without changing them.
type ListOpportunitiesOperation struct{ client *Client }

// ListOpportunities binds the Query to this client.
func (client *Client) ListOpportunities() ListOpportunitiesOperation {
	return ListOpportunitiesOperation{client}
}

// Definition returns the generated Query contract.
func (ListOpportunitiesOperation) Definition() sdkgo.QueryDefinition {
	return ListOpportunitiesDefinition
}

// Invoke reads one page through Copper's opportunities search endpoint.
func (operation ListOpportunitiesOperation) Invoke(call sdkgo.Call, input ListOpportunitiesInput) sdkgo.QueryAttempt[OpportunitiesPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 ||
		len(input.Name) > 255 || strings.ContainsAny(input.Name, "\r\n") {
		return listOpportunitiesBranch(ListOpportunitiesBranchDefect, sdkgo.FailureValidation, "invalid opportunities page or filter")
	}
	request := struct {
		PageNumber int    `json:"page_number,omitempty"`
		PageSize   int    `json:"page_size,omitempty"`
		Name       string `json:"name,omitempty"`
	}{PageNumber: input.PageNumber, PageSize: input.PageSize, Name: input.Name}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return listOpportunitiesBranch(ListOpportunitiesBranchDefect, sdkgo.FailureLocalDefect, "opportunities search could not be encoded")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodPost, "/opportunities/search", requestBody)
	if errors.Is(response.err, errCredential) {
		return listOpportunitiesBranch(ListOpportunitiesBranchDefect, sdkgo.FailureAuthentication, "Copper credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OpportunitiesPage](queryFailure("listOpportunities", sdkgo.FailureRateLimit, "Copper rate limited opportunities search"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[OpportunitiesPage](queryFailure("listOpportunities", sdkgo.FailureAvailability, "Copper opportunities search is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listOpportunitiesBranch(ListOpportunitiesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Copper opportunities response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OpportunitiesPage](queryFailure("listOpportunities", sdkgo.FailureTransport, "Copper opportunities response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return listOpportunitiesBranch(ListOpportunitiesBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return listOpportunitiesBranch(ListOpportunitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned invalid opportunities JSON")
	}
	page := OpportunitiesPage{Opportunities: make([]OpportunityMetadata, 0, len(records))}
	for _, record := range records {
		opportunity, decodeError := decodeOpportunity(record)
		if decodeError != nil {
			return listOpportunitiesBranch(ListOpportunitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned an invalid opportunity")
		}
		page.Opportunities = append(page.Opportunities, opportunity)
	}
	return sdkgo.NewQueryBranch(ListOpportunitiesBranchListed, page, nil, sdkgo.Receipt{})
}

func listOpportunitiesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OpportunitiesPage] {
	failure := queryFailure("listOpportunities", kind, message)
	return sdkgo.NewQueryBranch(branch, OpportunitiesPage{}, &failure, sdkgo.Receipt{})
}
