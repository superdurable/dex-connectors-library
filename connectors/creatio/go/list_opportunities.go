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

// OpportunitySummary contains the stable identity and name of one Creatio opportunity.
type OpportunitySummary struct {
	// ID is the Opportunity.Id UUID.
	ID string `json:"id"`
	// Name is the opportunity display name.
	Name string `json:"name"`
}

// ListOpportunitiesInput selects one bounded page of Creatio opportunities.
type ListOpportunitiesInput struct {
	// PageNumber starts at one; zero selects the first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero selects 20 records.
	PageSize int `json:"pageSize,omitempty"`
}

// OpportunitiesPage contains one Creatio OData result page.
type OpportunitiesPage struct {
	// Opportunities are the selected public opportunity fields.
	Opportunities []OpportunitySummary `json:"opportunities"`
}

// ListOpportunitiesOperation reads one OData page without changing records.
type ListOpportunitiesOperation struct{ client *Client }

// ListOpportunities binds the Query to this client.
func (client *Client) ListOpportunities() ListOpportunitiesOperation {
	return ListOpportunitiesOperation{client}
}

// Definition returns the generated Query contract.
func (ListOpportunitiesOperation) Definition() sdkgo.QueryDefinition {
	return ListOpportunitiesDefinition
}

// Invoke reads opportunity IDs and names from one OData page.
func (operation ListOpportunitiesOperation) Invoke(call sdkgo.Call, input ListOpportunitiesInput) sdkgo.QueryAttempt[OpportunitiesPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 {
		return opportunitiesPageBranch(ListOpportunitiesBranchDefect, sdkgo.FailureValidation, "invalid opportunity page")
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
			return opportunitiesPageBranch(ListOpportunitiesBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[OpportunitiesPage](opportunitiesFailure("listOpportunities", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	query := url.Values{"$select": {"Id,Name"}, "$top": {strconv.Itoa(pageSize)}, "$skip": {strconv.Itoa((pageNumber - 1) * pageSize)}, "$orderby": {"Id"}}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Opportunity?"+query.Encode(), nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OpportunitiesPage](opportunitiesFailure("listOpportunities", sdkgo.FailureRateLimit, "Creatio rate limited opportunities"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[OpportunitiesPage](opportunitiesFailure("listOpportunities", sdkgo.FailureAvailability, "Creatio opportunities are unavailable"), response.retryAfter)
	}
	if response.isBodyTooLarge {
		return opportunitiesPageBranch(ListOpportunitiesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio opportunities response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OpportunitiesPage](opportunitiesFailure("listOpportunities", sdkgo.FailureTransport, "Creatio opportunities response failed"), 0)
	}
	if response.status != http.StatusOK {
		return opportunitiesPageBranch(ListOpportunitiesBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected opportunities HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return opportunitiesPageBranch(ListOpportunitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid opportunities JSON")
	}
	page := OpportunitiesPage{Opportunities: make([]OpportunitySummary, 0, len(envelope.Value))}
	for _, record := range envelope.Value {
		opportunity, err := decodeOpportunityRecord(record)
		if err != nil {
			return opportunitiesPageBranch(ListOpportunitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned an invalid opportunity")
		}
		page.Opportunities = append(page.Opportunities, opportunity)
	}
	return sdkgo.NewQueryBranch(ListOpportunitiesBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeOpportunityRecord(body []byte) (OpportunitySummary, error) {
	var wire struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OpportunitySummary{}, err
	}
	if !contactUUIDPattern.MatchString(wire.ID) || strings.TrimSpace(wire.Name) == "" {
		return OpportunitySummary{}, errors.New("opportunity identity missing")
	}
	return OpportunitySummary{ID: wire.ID, Name: wire.Name}, nil
}

func opportunitiesFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "creatio", Operation: operation, Message: message}
}

func opportunitiesPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OpportunitiesPage] {
	failure := opportunitiesFailure("listOpportunities", kind, message)
	return sdkgo.NewQueryBranch(branch, OpportunitiesPage{}, &failure, sdkgo.Receipt{})
}
