// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookleadads

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListLeadFormsInput selects a Facebook Page and an optional pagination cursor.
type ListLeadFormsInput struct {
	// PageID is the numeric Page ID from Meta Business Suite.
	PageID string `json:"pageId"`
	// After is Meta's opaque cursor from a prior page; blank starts at the first page.
	After string `json:"after,omitempty"`
}

// LeadFormMetadata contains a form's public identity and status.
type LeadFormMetadata struct {
	// ID is the stable Meta lead-form ID.
	ID string `json:"id"`
	// Name is the untrusted display name.
	Name string `json:"name"`
	// Status is Meta's form status.
	Status string `json:"status"`
}

// LeadFormsPage contains one bounded page of lead forms.
type LeadFormsPage struct {
	// Forms are the form identities in provider order.
	Forms []LeadFormMetadata `json:"forms"`
	// NextCursor is blank when Meta reported no later page.
	NextCursor string `json:"nextCursor"`
}

// GetLeadInput selects one Meta lead.
type GetLeadInput struct {
	// LeadID is the numeric ID from a leadgen webhook or Ads Manager.
	LeadID string `json:"leadId"`
}

// LeadMetadata excludes form answers, which may contain sensitive personal data.
type LeadMetadata struct {
	// ID is the stable Meta lead ID.
	ID string `json:"id"`
	// FormID identifies the originating lead form.
	FormID string `json:"formId"`
	// AdID identifies the originating ad when present.
	AdID string `json:"adId"`
	// CreatedTime is Meta's raw lead creation timestamp.
	CreatedTime string `json:"createdTime"`
}

// ListLeadFormsOperation reads one Page lead-form page.
type ListLeadFormsOperation struct{ client *Client }

// GetLeadOperation reads one lead's identity metadata.
type GetLeadOperation struct{ client *Client }

// ListLeadForms binds the form Query to this client.
func (client *Client) ListLeadForms() ListLeadFormsOperation { return ListLeadFormsOperation{client} }

// GetLead binds the lead Query to this client.
func (client *Client) GetLead() GetLeadOperation { return GetLeadOperation{client} }

// Definition returns the generated form Query contract.
func (ListLeadFormsOperation) Definition() sdkgo.QueryDefinition { return ListLeadFormsDefinition }

// Definition returns the generated lead Query contract.
func (GetLeadOperation) Definition() sdkgo.QueryDefinition { return GetLeadDefinition }

var numericID = regexp.MustCompile(`^[0-9]{1,40}$`)

// Invoke lists forms while projecting only their ID, name, and status.
func (operation ListLeadFormsOperation) Invoke(call sdkgo.Call, input ListLeadFormsInput) sdkgo.QueryAttempt[LeadFormsPage] {
	if !numericID.MatchString(input.PageID) || len(input.After) > 4096 || strings.ContainsAny(input.After, "\r\n\x00") {
		return queryBranch("listLeadForms", ListLeadFormsBranchDefect, LeadFormsPage{}, sdkgo.FailureValidation, "Page ID or cursor is invalid")
	}
	query := url.Values{"fields": {"id,name,status"}, "limit": {"100"}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	response := operation.client.get(call, "/"+input.PageID+"/leadgen_forms?"+query.Encode())
	if decision, handled := classifyGraphResponse("listLeadForms", response, LeadFormsPage{}, ListLeadFormsBranchNotFound, ListLeadFormsBranchProviderRejected, ListLeadFormsBranchInvalidResponse, ListLeadFormsBranchDefect); handled {
		return decision
	}
	var wire struct {
		Data []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data == nil || len(wire.Data) > 100 || len(wire.Paging.Cursors.After) > 4096 {
		return queryBranch("listLeadForms", ListLeadFormsBranchInvalidResponse, LeadFormsPage{}, sdkgo.FailureProtocol, "Meta returned invalid lead-form JSON")
	}
	page := LeadFormsPage{Forms: make([]LeadFormMetadata, 0, len(wire.Data)), NextCursor: wire.Paging.Cursors.After}
	for _, form := range wire.Data {
		if !numericID.MatchString(form.ID) || form.Name == "" {
			return queryBranch("listLeadForms", ListLeadFormsBranchInvalidResponse, LeadFormsPage{}, sdkgo.FailureProtocol, "Meta returned incomplete form identity")
		}
		page.Forms = append(page.Forms, LeadFormMetadata{ID: form.ID, Name: form.Name, Status: form.Status})
	}
	return sdkgo.NewQueryBranch(ListLeadFormsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads lead metadata without requesting or persisting answers.
func (operation GetLeadOperation) Invoke(call sdkgo.Call, input GetLeadInput) sdkgo.QueryAttempt[LeadMetadata] {
	if !numericID.MatchString(input.LeadID) {
		return queryBranch("getLead", GetLeadBranchDefect, LeadMetadata{}, sdkgo.FailureValidation, "lead ID must be numeric")
	}
	query := url.Values{"fields": {"id,created_time,form_id,ad_id"}}
	response := operation.client.get(call, "/"+input.LeadID+"?"+query.Encode())
	if decision, handled := classifyGraphResponse("getLead", response, LeadMetadata{}, GetLeadBranchNotFound, GetLeadBranchProviderRejected, GetLeadBranchInvalidResponse, GetLeadBranchDefect); handled {
		return decision
	}
	var wire struct {
		ID          string `json:"id"`
		FormID      string `json:"form_id"`
		AdID        string `json:"ad_id"`
		CreatedTime string `json:"created_time"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.FormID) || wire.CreatedTime == "" || (wire.AdID != "" && !numericID.MatchString(wire.AdID)) {
		return queryBranch("getLead", GetLeadBranchInvalidResponse, LeadMetadata{}, sdkgo.FailureProtocol, "Meta returned invalid lead metadata")
	}
	return sdkgo.NewQueryBranch(GetLeadBranchFound, LeadMetadata{ID: wire.ID, FormID: wire.FormID, AdID: wire.AdID, CreatedTime: wire.CreatedTime}, nil, sdkgo.Receipt{})
}

func classifyGraphResponse[Output any](operation string, response graphResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureTransport, "Meta request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureAvailability, "Meta is temporarily unavailable"), response.retryAfter), true
	}
	if graphErr, ok := decodeGraphError(response.body); response.status >= 400 && response.err == nil && ok {
		if graphErr.isThrottled() {
			return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the request"), response.retryAfter), true
		}
		if graphErr.isTransient {
			return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureAvailability, "Meta reported a transient error"), response.retryAfter), true
		}
	}
	if response.status == http.StatusNotFound {
		return queryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "Meta did not find the resource"), true
	}
	if response.status != http.StatusOK {
		return queryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "Meta rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "Meta response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureTransport, "Meta response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func queryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func queryFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "facebook-lead-ads", Operation: operation, Message: message}
}
