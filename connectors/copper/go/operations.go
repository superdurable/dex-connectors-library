// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListLeadStatusesInput selects lead statuses visible to the connected Copper user.
type ListLeadStatusesInput struct{}

// LeadStatusMetadata identifies one Copper lead status without private fields.
type LeadStatusMetadata struct {
	// ID is the stable Copper status identifier.
	ID int64 `json:"id"`
	// Name is the status display name.
	Name string `json:"name"`
}

// LeadStatusesList contains visible lead status identities.
type LeadStatusesList struct {
	// Statuses contains only IDs and names.
	Statuses []LeadStatusMetadata `json:"statuses"`
}

// GetLeadInput selects a Copper lead by its numeric ID.
type GetLeadInput struct {
	// LeadID is the numeric ID shown on a Copper lead and returned by its API.
	LeadID string `json:"leadId"`
}

// LeadMetadata contains the lead's ID, name, status, and company name.
type LeadMetadata struct {
	// ID is the stable Copper lead identifier.
	ID int64 `json:"id"`
	// Name is the lead display name.
	Name string `json:"name"`
	// Status is the current lead status name.
	Status string `json:"status"`
	// CompanyName is the associated company name when available.
	CompanyName string `json:"companyName"`
}

// ListLeadStatusesOperation reads lead statuses from Copper.
type ListLeadStatusesOperation struct{ client *Client }

// GetLeadOperation reads one lead from Copper.
type GetLeadOperation struct{ client *Client }

// ListLeadStatuses binds status discovery to this client.
func (client *Client) ListLeadStatuses() ListLeadStatusesOperation {
	return ListLeadStatusesOperation{client}
}

// GetLead binds the lead Query to this client.
func (client *Client) GetLead() GetLeadOperation { return GetLeadOperation{client} }

// Definition returns the generated status Query contract.
func (ListLeadStatusesOperation) Definition() sdkgo.QueryDefinition {
	return ListLeadStatusesDefinition
}

// Definition returns the generated lead Query contract.
func (GetLeadOperation) Definition() sdkgo.QueryDefinition { return GetLeadDefinition }

// Invoke reads status IDs and names visible to the connected user.
func (operation ListLeadStatusesOperation) Invoke(call sdkgo.Call, _ ListLeadStatusesInput) sdkgo.QueryAttempt[LeadStatusesList] {
	branches := queryBranches{found: ListLeadStatusesBranchListed, rejected: ListLeadStatusesBranchProviderRejected, invalid: ListLeadStatusesBranchInvalidResponse, defect: ListLeadStatusesBranchDefect}
	return invokeQuery(call, operation.client, "listLeadStatuses", "/lead_statuses", branches, decodeLeadStatuses)
}

// Invoke reads one Copper lead without custom fields.
func (operation GetLeadOperation) Invoke(call sdkgo.Call, input GetLeadInput) sdkgo.QueryAttempt[LeadMetadata] {
	branches := queryBranches{found: GetLeadBranchFound, notFound: GetLeadBranchNotFound, rejected: GetLeadBranchProviderRejected, invalid: GetLeadBranchInvalidResponse, defect: GetLeadBranchDefect}
	leadID, err := strconv.ParseUint(input.LeadID, 10, 63)
	if err != nil || leadID == 0 || strings.TrimSpace(input.LeadID) != input.LeadID {
		return queryBranch("getLead", branches.defect, LeadMetadata{}, sdkgo.FailureValidation, "lead ID must be a positive decimal identifier")
	}
	return invokeQuery(call, operation.client, "getLead", "/leads/"+input.LeadID, branches, decodeLead)
}

func decodeLeadStatuses(body []byte) (LeadStatusesList, error) {
	var wire []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return LeadStatusesList{}, err
	}
	if wire == nil {
		return LeadStatusesList{}, fmt.Errorf("lead status list missing")
	}
	statuses := make([]LeadStatusMetadata, 0, len(wire))
	for _, status := range wire {
		if status.ID <= 0 || status.Name == "" {
			return LeadStatusesList{}, fmt.Errorf("lead status identity missing")
		}
		statuses = append(statuses, LeadStatusMetadata{ID: status.ID, Name: status.Name})
	}
	return LeadStatusesList{Statuses: statuses}, nil
}

func decodeLead(body []byte) (LeadMetadata, error) {
	var wire struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		CompanyName string `json:"company_name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return LeadMetadata{}, err
	}
	if wire.ID <= 0 || wire.Name == "" || wire.Status == "" {
		return LeadMetadata{}, fmt.Errorf("lead identity missing")
	}
	return LeadMetadata{ID: wire.ID, Name: wire.Name, Status: wire.Status, CompanyName: wire.CompanyName}, nil
}
