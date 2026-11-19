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

// ListLeadsInput selects one Copper search page with optional filters.
type ListLeadsInput struct {
	// PageNumber starts at one; zero uses Copper's first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero uses Copper's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// Name optionally matches a full lead name.
	Name string `json:"name,omitempty"`
	// Email optionally matches a lead's full email address.
	Email string `json:"email,omitempty"`
}

// LeadsPage contains one returned page of lead summaries.
type LeadsPage struct {
	// Leads are the records returned by Copper for this page.
	Leads []LeadMetadata `json:"leads"`
}

// ListLeadsOperation searches Copper leads without changing them.
type ListLeadsOperation struct{ client *Client }

// ListLeads binds the Query to this client.
func (client *Client) ListLeads() ListLeadsOperation { return ListLeadsOperation{client} }

// Definition returns the generated Query contract.
func (ListLeadsOperation) Definition() sdkgo.QueryDefinition { return ListLeadsDefinition }

// Invoke reads one page through Copper's leads search endpoint.
func (operation ListLeadsOperation) Invoke(call sdkgo.Call, input ListLeadsInput) sdkgo.QueryAttempt[LeadsPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 ||
		len(input.Name) > 255 || (input.Email != "" && !validLeadEmail(input.Email)) || strings.ContainsAny(input.Name, "\r\n") {
		return listLeadsBranch(ListLeadsBranchDefect, sdkgo.FailureValidation, "invalid leads page or filter")
	}
	request := struct {
		PageNumber int    `json:"page_number,omitempty"`
		PageSize   int    `json:"page_size,omitempty"`
		Name       string `json:"name,omitempty"`
		Emails     string `json:"emails,omitempty"`
	}{PageNumber: input.PageNumber, PageSize: input.PageSize, Name: input.Name}
	if input.Email != "" {
		request.Emails = input.Email
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return listLeadsBranch(ListLeadsBranchDefect, sdkgo.FailureLocalDefect, "leads search could not be encoded")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodPost, "/leads/search", requestBody)
	if errors.Is(response.err, errCredential) {
		return listLeadsBranch(ListLeadsBranchDefect, sdkgo.FailureAuthentication, "Copper credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[LeadsPage](queryFailure("listLeads", sdkgo.FailureRateLimit, "Copper rate limited leads search"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[LeadsPage](queryFailure("listLeads", sdkgo.FailureAvailability, "Copper leads search is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listLeadsBranch(ListLeadsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Copper leads response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[LeadsPage](queryFailure("listLeads", sdkgo.FailureTransport, "Copper leads response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return listLeadsBranch(ListLeadsBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return listLeadsBranch(ListLeadsBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned invalid leads JSON")
	}
	page := LeadsPage{Leads: make([]LeadMetadata, 0, len(records))}
	for _, record := range records {
		lead, decodeError := decodeLead(record)
		if decodeError != nil {
			return listLeadsBranch(ListLeadsBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned an invalid lead")
		}
		page.Leads = append(page.Leads, lead)
	}
	return sdkgo.NewQueryBranch(ListLeadsBranchListed, page, nil, sdkgo.Receipt{})
}

func listLeadsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LeadsPage] {
	failure := queryFailure("listLeads", kind, message)
	return sdkgo.NewQueryBranch(branch, LeadsPage{}, &failure, sdkgo.Receipt{})
}
