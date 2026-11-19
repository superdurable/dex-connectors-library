// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetLeadInput identifies one Creatio lead by UUID.
type GetLeadInput struct {
	// LeadID is the Lead.Id UUID.
	LeadID string `json:"leadId"`
}

// GetLeadOperation reads one lead through OData 4.
type GetLeadOperation struct{ client *Client }

// GetLead binds the Query to this client.
func (client *Client) GetLead() GetLeadOperation { return GetLeadOperation{client} }

// Definition returns the generated Query contract.
func (GetLeadOperation) Definition() sdkgo.QueryDefinition { return GetLeadDefinition }

// Invoke reads an lead's stable ID and name.
func (operation GetLeadOperation) Invoke(call sdkgo.Call, input GetLeadInput) sdkgo.QueryAttempt[LeadSummary] {
	if !contactUUIDPattern.MatchString(input.LeadID) {
		return leadQueryBranch(GetLeadBranchDefect, sdkgo.FailureValidation, "invalid lead UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return leadQueryBranch(GetLeadBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[LeadSummary](leadsFailure("getLead", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Lead("+input.LeadID+")?$select=Id%2CLeadName", nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[LeadSummary](leadsFailure("getLead", sdkgo.FailureRateLimit, "Creatio rate limited lead lookup"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[LeadSummary](leadsFailure("getLead", sdkgo.FailureAvailability, "Creatio lead lookup unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return leadQueryBranch(GetLeadBranchNotFound, sdkgo.FailureNotFound, "Creatio lead was not found")
	}
	if response.isBodyTooLarge {
		return leadQueryBranch(GetLeadBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio lead response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[LeadSummary](leadsFailure("getLead", sdkgo.FailureTransport, "Creatio lead response failed"), 0)
	}
	if response.status != http.StatusOK {
		return leadQueryBranch(GetLeadBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	lead, err := decodeLeadRecord(response.body)
	if err != nil || lead.ID != input.LeadID {
		return leadQueryBranch(GetLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid lead JSON")
	}
	return sdkgo.NewQueryBranch(GetLeadBranchFound, lead, nil, sdkgo.Receipt{})
}

func leadQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LeadSummary] {
	failure := leadsFailure("getLead", kind, message)
	return sdkgo.NewQueryBranch(branch, LeadSummary{}, &failure, sdkgo.Receipt{})
}
