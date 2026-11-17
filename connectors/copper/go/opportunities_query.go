// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// OpportunityMetadata contains the public identity and status of one Copper opportunity.
type OpportunityMetadata struct {
	// ID is the stable Copper opportunity identifier.
	ID int64 `json:"id"`
	// Name is the opportunity display name.
	Name string `json:"name"`
	// Status is the current opportunity status.
	Status string `json:"status"`
}

// GetOpportunityInput identifies one opportunity by Copper ID.
type GetOpportunityInput struct {
	// OpportunityID is a positive decimal identifier.
	OpportunityID string `json:"opportunityId"`
}

// GetOpportunityOperation reads one opportunity.
type GetOpportunityOperation struct{ client *Client }

// GetOpportunity binds the Query to this client.
func (client *Client) GetOpportunity() GetOpportunityOperation {
	return GetOpportunityOperation{client}
}

// Definition returns the generated Query contract.
func (GetOpportunityOperation) Definition() sdkgo.QueryDefinition { return GetOpportunityDefinition }

// Invoke reads one opportunity without custom fields.
func (operation GetOpportunityOperation) Invoke(call sdkgo.Call, input GetOpportunityInput) sdkgo.QueryAttempt[OpportunityMetadata] {
	branches := queryBranches{found: GetOpportunityBranchFound, notFound: GetOpportunityBranchNotFound, rejected: GetOpportunityBranchProviderRejected, invalid: GetOpportunityBranchInvalidResponse, defect: GetOpportunityBranchDefect}
	if !validOpportunityID(input.OpportunityID) {
		return queryBranch("getOpportunity", branches.defect, OpportunityMetadata{}, sdkgo.FailureValidation, "opportunity ID must be positive")
	}
	return invokeQuery(call, operation.client, "getOpportunity", "/opportunities/"+input.OpportunityID, branches, decodeOpportunity)
}

func decodeOpportunity(body []byte) (OpportunityMetadata, error) {
	var wire struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OpportunityMetadata{}, err
	}
	if wire.ID <= 0 || strings.TrimSpace(wire.Name) == "" || strings.TrimSpace(wire.Status) == "" {
		return OpportunityMetadata{}, fmt.Errorf("opportunity identity missing")
	}
	return OpportunityMetadata{ID: wire.ID, Name: wire.Name, Status: wire.Status}, nil
}
