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

// LeadSummary contains the stable identity and name of one Creatio lead.
type LeadSummary struct {
	// ID is the Lead.Id UUID.
	ID string `json:"id"`
	// Name is the lead display name.
	Name string `json:"name"`
}

// ListLeadsInput selects one bounded page of Creatio leads.
type ListLeadsInput struct {
	// PageNumber starts at one; zero selects the first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero selects 20 records.
	PageSize int `json:"pageSize,omitempty"`
}

// LeadsPage contains one Creatio OData result page.
type LeadsPage struct {
	// Leads are the selected public lead fields.
	Leads []LeadSummary `json:"leads"`
}

// ListLeadsOperation reads one OData page without changing records.
type ListLeadsOperation struct{ client *Client }

// ListLeads binds the Query to this client.
func (client *Client) ListLeads() ListLeadsOperation { return ListLeadsOperation{client} }

// Definition returns the generated Query contract.
func (ListLeadsOperation) Definition() sdkgo.QueryDefinition { return ListLeadsDefinition }

// Invoke reads lead IDs and names from one OData page.
func (operation ListLeadsOperation) Invoke(call sdkgo.Call, input ListLeadsInput) sdkgo.QueryAttempt[LeadsPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 {
		return leadsPageBranch(ListLeadsBranchDefect, sdkgo.FailureValidation, "invalid lead page")
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
			return leadsPageBranch(ListLeadsBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[LeadsPage](leadsFailure("listLeads", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	query := url.Values{"$select": {"Id,LeadName"}, "$top": {strconv.Itoa(pageSize)}, "$skip": {strconv.Itoa((pageNumber - 1) * pageSize)}, "$orderby": {"Id"}}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Lead?"+query.Encode(), nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[LeadsPage](leadsFailure("listLeads", sdkgo.FailureRateLimit, "Creatio rate limited leads"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[LeadsPage](leadsFailure("listLeads", sdkgo.FailureAvailability, "Creatio leads are unavailable"), response.retryAfter)
	}
	if response.isBodyTooLarge {
		return leadsPageBranch(ListLeadsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio leads response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[LeadsPage](leadsFailure("listLeads", sdkgo.FailureTransport, "Creatio leads response failed"), 0)
	}
	if response.status != http.StatusOK {
		return leadsPageBranch(ListLeadsBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected leads HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return leadsPageBranch(ListLeadsBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid leads JSON")
	}
	page := LeadsPage{Leads: make([]LeadSummary, 0, len(envelope.Value))}
	for _, record := range envelope.Value {
		lead, err := decodeLeadRecord(record)
		if err != nil {
			return leadsPageBranch(ListLeadsBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned an invalid lead")
		}
		page.Leads = append(page.Leads, lead)
	}
	return sdkgo.NewQueryBranch(ListLeadsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeLeadRecord(body []byte) (LeadSummary, error) {
	var wire struct {
		ID   string `json:"Id"`
		Name string `json:"LeadName"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return LeadSummary{}, err
	}
	if !contactUUIDPattern.MatchString(wire.ID) || strings.TrimSpace(wire.Name) == "" {
		return LeadSummary{}, errors.New("lead identity missing")
	}
	return LeadSummary{ID: wire.ID, Name: wire.Name}, nil
}

func leadsFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "creatio", Operation: operation, Message: message}
}

func leadsPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LeadsPage] {
	failure := leadsFailure("listLeads", kind, message)
	return sdkgo.NewQueryBranch(branch, LeadsPage{}, &failure, sdkgo.Receipt{})
}
