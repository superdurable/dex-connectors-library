// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sell

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListLeadsInput selects one bounded Zendesk Sell lead page.
type ListLeadsInput struct {
	// Page is the one-based page number.
	Page int `json:"page"`
	// PerPage is the number of leads to return, from 1 to 100.
	PerPage int `json:"perPage"`
}

// LeadMetadata contains lead identity without custom fields or links.
type LeadMetadata struct {
	// ID is the stable Zendesk Sell lead identifier.
	ID int64 `json:"id"`
	// FirstName is the lead's given name when available.
	FirstName string `json:"firstName"`
	// LastName is the lead's family name when available.
	LastName string `json:"lastName"`
	// EmailAddress is the lead's email address when available.
	EmailAddress string `json:"emailAddress"`
	// OrganizationName is the lead's organization when available.
	OrganizationName string `json:"organizationName"`
}

// LeadsPage contains one requested page of lead identities.
type LeadsPage struct {
	// Leads contains only identity fields from the provider response.
	Leads []LeadMetadata `json:"leads"`
	// Page is the one-based page number requested from the provider.
	Page int `json:"page"`
	// PerPage is the requested maximum page size.
	PerPage int `json:"perPage"`
}

// GetLeadInput selects a lead by its positive numeric ID.
type GetLeadInput struct {
	// LeadID is the decimal ID shown in Zendesk Sell and its API.
	LeadID string `json:"leadId"`
}

// ListLeadsOperation reads a bounded lead page.
type ListLeadsOperation struct{ client *Client }

// GetLeadOperation reads one lead identity.
type GetLeadOperation struct{ client *Client }

// ListLeads binds the lead page Query to this client.
func (client *Client) ListLeads() ListLeadsOperation { return ListLeadsOperation{client} }

// GetLead binds the lead identity Query to this client.
func (client *Client) GetLead() GetLeadOperation { return GetLeadOperation{client} }

// Definition returns the generated lead page Query contract.
func (ListLeadsOperation) Definition() sdkgo.QueryDefinition { return ListLeadsDefinition }

// Definition returns the generated lead identity Query contract.
func (GetLeadOperation) Definition() sdkgo.QueryDefinition { return GetLeadDefinition }

// Invoke reads a lead page without retaining custom fields or links.
func (operation ListLeadsOperation) Invoke(call sdkgo.Call, input ListLeadsInput) sdkgo.QueryAttempt[LeadsPage] {
	branches := queryBranches{found: ListLeadsBranchListed, rejected: ListLeadsBranchProviderRejected, invalid: ListLeadsBranchInvalidResponse, defect: ListLeadsBranchDefect}
	if input.Page < 1 || input.PerPage < 1 || input.PerPage > 100 {
		return queryBranch("listLeads", branches.defect, LeadsPage{}, sdkgo.FailureValidation, "lead page number or size is invalid")
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per_page": {strconv.Itoa(input.PerPage)}}
	return invokeQuery(call, operation.client, "listLeads", "/leads?"+query.Encode(), branches, func(body []byte) (LeadsPage, error) { return decodeLeadsPage(body, input) })
}

// Invoke reads one lead by ID without retaining custom fields or links.
func (operation GetLeadOperation) Invoke(call sdkgo.Call, input GetLeadInput) sdkgo.QueryAttempt[LeadMetadata] {
	branches := queryBranches{found: GetLeadBranchFound, notFound: GetLeadBranchNotFound, rejected: GetLeadBranchProviderRejected, invalid: GetLeadBranchInvalidResponse, defect: GetLeadBranchDefect}
	if !isPositiveLeadID(input.LeadID) {
		return queryBranch("getLead", branches.defect, LeadMetadata{}, sdkgo.FailureValidation, "lead ID must be a positive decimal identifier")
	}
	return invokeQuery(call, operation.client, "getLead", "/leads/"+input.LeadID, branches, decodeLead)
}

func isPositiveLeadID(leadID string) bool {
	if leadID == "" {
		return false
	}
	for _, digit := range leadID {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	value, err := strconv.ParseUint(leadID, 10, 63)
	return err == nil && value > 0
}

func decodeLeadsPage(body []byte, input ListLeadsInput) (LeadsPage, error) {
	var wire struct {
		Items []struct {
			Data json.RawMessage `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return LeadsPage{}, err
	}
	if wire.Items == nil {
		return LeadsPage{}, fmt.Errorf("lead page missing")
	}
	leads := make([]LeadMetadata, 0, len(wire.Items))
	for _, item := range wire.Items {
		lead, err := decodeLeadData(item.Data)
		if err != nil {
			return LeadsPage{}, err
		}
		leads = append(leads, lead)
	}
	return LeadsPage{Leads: leads, Page: input.Page, PerPage: input.PerPage}, nil
}

func decodeLead(body []byte) (LeadMetadata, error) {
	var wire struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return LeadMetadata{}, err
	}
	return decodeLeadData(wire.Data)
}

func decodeLeadData(body []byte) (LeadMetadata, error) {
	var wire struct {
		ID               int64  `json:"id"`
		FirstName        string `json:"first_name"`
		LastName         string `json:"last_name"`
		EmailAddress     string `json:"email"`
		OrganizationName string `json:"organization_name"`
	}
	if len(body) == 0 || string(body) == "null" {
		return LeadMetadata{}, fmt.Errorf("lead identity missing")
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return LeadMetadata{}, err
	}
	if wire.ID <= 0 {
		return LeadMetadata{}, fmt.Errorf("lead identity missing")
	}
	return LeadMetadata{ID: wire.ID, FirstName: wire.FirstName, LastName: wire.LastName, EmailAddress: wire.EmailAddress, OrganizationName: wire.OrganizationName}, nil
}
