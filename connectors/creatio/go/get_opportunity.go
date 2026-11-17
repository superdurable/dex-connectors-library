// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetOpportunityInput identifies one Creatio opportunity by UUID.
type GetOpportunityInput struct {
	// OpportunityID is the Opportunity.Id UUID.
	OpportunityID string `json:"opportunityId"`
}

// GetOpportunityOperation reads one opportunity through OData 4.
type GetOpportunityOperation struct{ client *Client }

// GetOpportunity binds the Query to this client.
func (client *Client) GetOpportunity() GetOpportunityOperation {
	return GetOpportunityOperation{client}
}

// Definition returns the generated Query contract.
func (GetOpportunityOperation) Definition() sdkgo.QueryDefinition { return GetOpportunityDefinition }

// Invoke reads an opportunity's stable ID and name.
func (operation GetOpportunityOperation) Invoke(call sdkgo.Call, input GetOpportunityInput) sdkgo.QueryAttempt[OpportunitySummary] {
	if !contactUUIDPattern.MatchString(input.OpportunityID) {
		return opportunityQueryBranch(GetOpportunityBranchDefect, sdkgo.FailureValidation, "invalid opportunity UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return opportunityQueryBranch(GetOpportunityBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[OpportunitySummary](opportunitiesFailure("getOpportunity", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Opportunity("+input.OpportunityID+")?$select=Id%2CName", nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OpportunitySummary](opportunitiesFailure("getOpportunity", sdkgo.FailureRateLimit, "Creatio rate limited opportunity lookup"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[OpportunitySummary](opportunitiesFailure("getOpportunity", sdkgo.FailureAvailability, "Creatio opportunity lookup unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return opportunityQueryBranch(GetOpportunityBranchNotFound, sdkgo.FailureNotFound, "Creatio opportunity was not found")
	}
	if response.isBodyTooLarge {
		return opportunityQueryBranch(GetOpportunityBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio opportunity response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OpportunitySummary](opportunitiesFailure("getOpportunity", sdkgo.FailureTransport, "Creatio opportunity response failed"), 0)
	}
	if response.status != http.StatusOK {
		return opportunityQueryBranch(GetOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	opportunity, err := decodeOpportunityRecord(response.body)
	if err != nil || !strings.EqualFold(opportunity.ID, input.OpportunityID) {
		return opportunityQueryBranch(GetOpportunityBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid opportunity JSON")
	}
	return sdkgo.NewQueryBranch(GetOpportunityBranchFound, opportunity, nil, sdkgo.Receipt{})
}

func opportunityQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OpportunitySummary] {
	failure := opportunitiesFailure("getOpportunity", kind, message)
	return sdkgo.NewQueryBranch(branch, OpportunitySummary{}, &failure, sdkgo.Receipt{})
}
