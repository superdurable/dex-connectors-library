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

// ListCompaniesInput selects one Copper search page with optional filters.
type ListCompaniesInput struct {
	// PageNumber starts at one; zero uses Copper's first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero uses Copper's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// Name optionally matches a full company name.
	Name string `json:"name,omitempty"`
	// EmailDomain optionally matches a company's unique email domain.
	EmailDomain string `json:"emailDomain,omitempty"`
}

// CompaniesPage contains one returned page of company summaries.
type CompaniesPage struct {
	// Companies are the records returned by Copper for this page.
	Companies []CompanyMetadata `json:"companies"`
}

// ListCompaniesOperation searches Copper companies without changing them.
type ListCompaniesOperation struct{ client *Client }

// ListCompanies binds the Query to this client.
func (client *Client) ListCompanies() ListCompaniesOperation { return ListCompaniesOperation{client} }

// Definition returns the generated Query contract.
func (ListCompaniesOperation) Definition() sdkgo.QueryDefinition { return ListCompaniesDefinition }

// Invoke reads one page through Copper's companies search endpoint.
func (operation ListCompaniesOperation) Invoke(call sdkgo.Call, input ListCompaniesInput) sdkgo.QueryAttempt[CompaniesPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 ||
		len(input.Name) > 255 || (input.EmailDomain != "" && !validCompanyDomain(input.EmailDomain)) || strings.ContainsAny(input.Name, "\r\n") {
		return listCompaniesBranch(ListCompaniesBranchDefect, sdkgo.FailureValidation, "invalid companies page or filter")
	}
	request := struct {
		PageNumber   int    `json:"page_number,omitempty"`
		PageSize     int    `json:"page_size,omitempty"`
		Name         string `json:"name,omitempty"`
		EmailDomains string `json:"email_domains,omitempty"`
	}{PageNumber: input.PageNumber, PageSize: input.PageSize, Name: input.Name}
	if input.EmailDomain != "" {
		request.EmailDomains = input.EmailDomain
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return listCompaniesBranch(ListCompaniesBranchDefect, sdkgo.FailureLocalDefect, "companies search could not be encoded")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodPost, "/companies/search", requestBody)
	if errors.Is(response.err, errCredential) {
		return listCompaniesBranch(ListCompaniesBranchDefect, sdkgo.FailureAuthentication, "Copper credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CompaniesPage](queryFailure("listCompanies", sdkgo.FailureRateLimit, "Copper rate limited companies search"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CompaniesPage](queryFailure("listCompanies", sdkgo.FailureAvailability, "Copper companies search is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listCompaniesBranch(ListCompaniesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Copper companies response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CompaniesPage](queryFailure("listCompanies", sdkgo.FailureTransport, "Copper companies response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return listCompaniesBranch(ListCompaniesBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return listCompaniesBranch(ListCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned invalid companies JSON")
	}
	page := CompaniesPage{Companies: make([]CompanyMetadata, 0, len(records))}
	for _, record := range records {
		company, decodeError := decodeCompany(record)
		if decodeError != nil {
			return listCompaniesBranch(ListCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned an invalid company")
		}
		page.Companies = append(page.Companies, company)
	}
	return sdkgo.NewQueryBranch(ListCompaniesBranchListed, page, nil, sdkgo.Receipt{})
}

func listCompaniesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CompaniesPage] {
	failure := queryFailure("listCompanies", kind, message)
	return sdkgo.NewQueryBranch(branch, CompaniesPage{}, &failure, sdkgo.Receipt{})
}
