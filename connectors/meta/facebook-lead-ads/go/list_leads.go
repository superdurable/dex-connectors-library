// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookleadads

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListLeadsInput selects one page of leads from a Page lead form.
type ListLeadsInput struct {
	// FormID is the numeric form ID returned by listLeadForms.
	FormID string `json:"formId"`
	// After is an opaque cursor from an earlier page; blank starts at the first page.
	After string `json:"after,omitempty"`
}

// LeadsPage contains one ordered page of lead identities without form answers.
type LeadsPage struct {
	// Leads are the lead identities returned by Meta.
	Leads []LeadMetadata `json:"leads"`
	// NextCursor is blank when there is no later page.
	NextCursor string `json:"nextCursor"`
}

// ListLeadsOperation reads lead identities from one form.
type ListLeadsOperation struct{ client *Client }

// ListLeads binds the Query to this client.
func (client *Client) ListLeads() ListLeadsOperation { return ListLeadsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListLeadsOperation) Definition() sdkgo.QueryDefinition { return ListLeadsDefinition }

// Invoke reads one page without requesting answer fields that can contain secrets.
func (operation ListLeadsOperation) Invoke(call sdkgo.Call, input ListLeadsInput) sdkgo.QueryAttempt[LeadsPage] {
	if !numericID.MatchString(input.FormID) || len(input.After) > 4096 || strings.ContainsAny(input.After, "\r\n\x00") {
		return queryBranch("listLeads", ListLeadsBranchDefect, LeadsPage{}, sdkgo.FailureValidation, "form ID or cursor is invalid")
	}
	query := url.Values{"fields": {"id,created_time,form_id,ad_id"}, "limit": {"100"}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	response := operation.client.get(call, "/"+input.FormID+"/leads?"+query.Encode())
	if decision, handled := classifyGraphResponse("listLeads", response, LeadsPage{}, ListLeadsBranchNotFound, ListLeadsBranchProviderRejected, ListLeadsBranchInvalidResponse, ListLeadsBranchDefect); handled {
		return decision
	}
	var wire struct {
		Data []struct {
			ID          string `json:"id"`
			FormID      string `json:"form_id"`
			AdID        string `json:"ad_id"`
			CreatedTime string `json:"created_time"`
		} `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
		} `json:"paging"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Data) > 100 || len(wire.Paging.Cursors.After) > 4096 {
		return queryBranch("listLeads", ListLeadsBranchInvalidResponse, LeadsPage{}, sdkgo.FailureProtocol, "Meta returned invalid lead list JSON")
	}
	leads := make([]LeadMetadata, 0, len(wire.Data))
	for _, lead := range wire.Data {
		if !numericID.MatchString(lead.ID) || lead.FormID != input.FormID || lead.CreatedTime == "" ||
			(lead.AdID != "" && !numericID.MatchString(lead.AdID)) {
			return queryBranch("listLeads", ListLeadsBranchInvalidResponse, LeadsPage{}, sdkgo.FailureProtocol, "Meta returned incomplete lead identity")
		}
		leads = append(leads, LeadMetadata{ID: lead.ID, FormID: lead.FormID, AdID: lead.AdID, CreatedTime: lead.CreatedTime})
	}
	return sdkgo.NewQueryBranch(ListLeadsBranchListed, LeadsPage{Leads: leads, NextCursor: wire.Paging.Cursors.After}, nil, sdkgo.Receipt{})
}
