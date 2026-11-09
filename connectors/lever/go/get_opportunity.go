// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetOpportunityInput selects an opportunity by its non-secret Lever ID.
type GetOpportunityInput struct {
	// OpportunityID is the ID shown in Lever Opportunities or returned by the API.
	OpportunityID string `json:"opportunityId"`
}

// OpportunityStage excludes contact details, resumes, and notes.
type OpportunityStage struct {
	// ID is the Lever opportunity UID.
	ID string `json:"id"`
	// Name is the candidate display name.
	Name string `json:"name"`
	// Stage is the current pipeline stage UID.
	Stage string `json:"stage"`
}

// GetOpportunityOperation reads one opportunity's identity and state.
type GetOpportunityOperation struct{ client *Client }

// GetOpportunity binds the Query to this client.
func (client *Client) GetOpportunity() GetOpportunityOperation {
	return GetOpportunityOperation{client}
}

// Definition returns the manifest-generated Query opportunity.
func (GetOpportunityOperation) Definition() sdkgo.QueryDefinition { return GetOpportunityDefinition }

// Invoke reads one opportunity and classifies retryable and definitive outcomes.
func (operation GetOpportunityOperation) Invoke(call sdkgo.Call, input GetOpportunityInput) sdkgo.QueryAttempt[OpportunityStage] {
	if !validOpportunityID(input.OpportunityID) {
		return opportunityBranch(GetOpportunityBranchDefect, sdkgo.FailureValidation, "opportunity ID must be a UUID")
	}
	response := operation.client.get(call, "/opportunities/"+url.PathEscape(input.OpportunityID))
	if errors.Is(response.err, errCredential) {
		return opportunityBranch(GetOpportunityBranchDefect, sdkgo.FailureAuthentication, "Lever credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[OpportunityStage](opportunityFailure(sdkgo.FailureTransport, "Lever request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OpportunityStage](opportunityFailure(sdkgo.FailureRateLimit, "Lever rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[OpportunityStage](opportunityFailure(sdkgo.FailureAvailability, "Lever is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return opportunityBranch(GetOpportunityBranchNotFound, sdkgo.FailureNotFound, "Lever did not expose the opportunity")
	}
	if response.status != http.StatusOK {
		return opportunityBranch(GetOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return opportunityBranch(GetOpportunityBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lever response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OpportunityStage](opportunityFailure(sdkgo.FailureTransport, "Lever response was interrupted"), 0)
	}
	opportunityStage, err := decodeOpportunity(response.body, input.OpportunityID)
	if err != nil {
		return opportunityBranch(GetOpportunityBranchInvalidResponse, sdkgo.FailureProtocol, "Lever returned invalid opportunity JSON")
	}
	return sdkgo.NewQueryBranch(GetOpportunityBranchFound, opportunityStage, nil, sdkgo.Receipt{})
}

var opportunityIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func validOpportunityID(opportunityID string) bool {
	return opportunityIDPattern.MatchString(opportunityID)
}

func decodeOpportunity(body []byte, requestedOpportunityID string) (OpportunityStage, error) {
	var wire struct {
		Data struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Stage string `json:"stage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OpportunityStage{}, err
	}
	if wire.Data.ID != requestedOpportunityID || strings.TrimSpace(wire.Data.Name) == "" || strings.TrimSpace(wire.Data.Stage) == "" {
		return OpportunityStage{}, fmt.Errorf("opportunity identity, name, or stage missing")
	}
	return OpportunityStage{ID: wire.Data.ID, Name: wire.Data.Name, Stage: wire.Data.Stage}, nil
}

func opportunityFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: "getOpportunity", Message: message}
}

func opportunityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OpportunityStage] {
	failure := opportunityFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, OpportunityStage{}, &failure, sdkgo.Receipt{})
}
