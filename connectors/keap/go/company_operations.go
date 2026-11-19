// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CompanyIDInput selects a company by its Keap ID.
type CompanyIDInput struct {
	// CompanyID is the company ID shown in Keap CRM.
	CompanyID string `json:"companyId"`
}

// CompanyNameInput supplies the display name of a new company.
type CompanyNameInput struct {
	// Name is the non-empty company name.
	Name string `json:"name"`
}

// UpdateCompanyInput replaces the name of an existing company.
type UpdateCompanyInput struct {
	// CompanyID is the existing company ID.
	CompanyID string `json:"companyId"`
	// Name is the replacement company name.
	Name string `json:"name"`
}

// ListCompaniesInput selects a bounded page of companies.
type ListCompaniesInput struct {
	// PageSize is the maximum number of companies; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the opaque token from the preceding page.
	PageToken string `json:"pageToken,omitempty"`
}

// CompanyProfile contains the identity and display name of a company.
type CompanyProfile struct {
	// ID is Keap's stable company ID.
	ID string `json:"id"`
	// Name is the company's display name.
	Name string `json:"name"`
}

// CompaniesPage contains a page of companies and its continuation token.
type CompaniesPage struct {
	// Companies are the returned company identities and names.
	Companies []CompanyProfile `json:"companies"`
	// NextPageToken selects the following page, when present.
	NextPageToken string `json:"nextPageToken"`
}

// CompanyDeletion confirms deletion of a company.
type CompanyDeletion struct {
	// CompanyID is the deleted company ID.
	CompanyID string `json:"companyId"`
	// Deleted reports Keap's acceptance of the deletion.
	Deleted bool `json:"deleted"`
}

// GetCompanyOperation reads one company.
type GetCompanyOperation struct{ client *Client }

// ListCompaniesOperation reads one company page.
type ListCompaniesOperation struct{ client *Client }

// CreateCompanyOperation creates a company with bounded sends.
type CreateCompanyOperation struct{ client *Client }

// UpdateCompanyOperation replaces a company's name.
type UpdateCompanyOperation struct{ client *Client }

// DeleteCompanyOperation removes a company.
type DeleteCompanyOperation struct{ client *Client }

// GetCompany binds a company lookup to the client.
func (client *Client) GetCompany() GetCompanyOperation { return GetCompanyOperation{client} }

// ListCompanies binds a company list to the client.
func (client *Client) ListCompanies() ListCompaniesOperation { return ListCompaniesOperation{client} }

// CreateCompany binds company creation to the client.
func (client *Client) CreateCompany() CreateCompanyOperation { return CreateCompanyOperation{client} }

// UpdateCompany binds a company update to the client.
func (client *Client) UpdateCompany() UpdateCompanyOperation { return UpdateCompanyOperation{client} }

// DeleteCompany binds company deletion to the client.
func (client *Client) DeleteCompany() DeleteCompanyOperation { return DeleteCompanyOperation{client} }

// Definition returns the generated query contract.
func (GetCompanyOperation) Definition() sdkgo.QueryDefinition { return GetCompanyDefinition }

// Definition returns the generated query contract.
func (ListCompaniesOperation) Definition() sdkgo.QueryDefinition { return ListCompaniesDefinition }

// Definition returns the generated mutation contract.
func (CreateCompanyOperation) Definition() sdkgo.MutationDefinition { return CreateCompanyDefinition }

// Definition returns the generated mutation contract.
func (UpdateCompanyOperation) Definition() sdkgo.MutationDefinition { return UpdateCompanyDefinition }

// Definition returns the generated mutation contract.
func (DeleteCompanyOperation) Definition() sdkgo.MutationDefinition { return DeleteCompanyDefinition }

// IdempotencyKey derives the stable Dex call identity.
func (CreateCompanyOperation) IdempotencyKey(callID sdkgo.CallID, _ CompanyNameInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (UpdateCompanyOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCompanyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (DeleteCompanyOperation) IdempotencyKey(callID sdkgo.CallID, _ CompanyIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads a company by ID.
func (operation GetCompanyOperation) Invoke(call sdkgo.Call, input CompanyIDInput) sdkgo.QueryAttempt[CompanyProfile] {
	if !validContactID(input.CompanyID) {
		return keapQueryBranch("getCompany", GetCompanyBranchDefect, CompanyProfile{}, sdkgo.FailureValidation, "company ID is invalid")
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/companies/"+input.CompanyID+"?fields=id%2Ccompany_name", nil)
	if response.status == http.StatusNotFound {
		return keapQueryBranch("getCompany", GetCompanyBranchNotFound, CompanyProfile{}, sdkgo.FailureNotFound, "Keap did not find the company")
	}
	if branch, retry, handled := classifyKeapQuery(response, "getCompany", GetCompanyBranchProviderRejected, GetCompanyBranchInvalidResponse, GetCompanyBranchDefect, CompanyProfile{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	company, ok := decodeCompany(response.body)
	if !ok {
		return keapQueryBranch("getCompany", GetCompanyBranchInvalidResponse, CompanyProfile{}, sdkgo.FailureProtocol, "Keap returned invalid company JSON")
	}
	return sdkgo.NewQueryBranch(GetCompanyBranchFound, company, nil, sdkgo.Receipt{})
}

// Invoke reads one bounded page of companies.
func (operation ListCompaniesOperation) Invoke(call sdkgo.Call, input ListCompaniesInput) sdkgo.QueryAttempt[CompaniesPage] {
	if input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return keapQueryBranch("listCompanies", ListCompaniesBranchDefect, CompaniesPage{}, sdkgo.FailureValidation, "page size or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"fields": {"id,company_name"}, "page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/companies?"+query.Encode(), nil)
	if branch, retry, handled := classifyKeapQuery(response, "listCompanies", ListCompaniesBranchProviderRejected, ListCompaniesBranchInvalidResponse, ListCompaniesBranchDefect, CompaniesPage{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	var wire struct {
		Companies     []json.RawMessage `json:"companies"`
		NextPageToken string            `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Companies == nil {
		return keapQueryBranch("listCompanies", ListCompaniesBranchInvalidResponse, CompaniesPage{}, sdkgo.FailureProtocol, "Keap returned invalid company list JSON")
	}
	page := CompaniesPage{Companies: make([]CompanyProfile, 0, len(wire.Companies)), NextPageToken: wire.NextPageToken}
	for _, item := range wire.Companies {
		company, ok := decodeCompany(item)
		if !ok {
			return keapQueryBranch("listCompanies", ListCompaniesBranchInvalidResponse, CompaniesPage{}, sdkgo.FailureProtocol, "Keap returned invalid company identity")
		}
		page.Companies = append(page.Companies, company)
	}
	return sdkgo.NewQueryBranch(ListCompaniesBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates one company, retrying an unknown result only within the send cap.
func (operation CreateCompanyOperation) Invoke(call sdkgo.Call, input CompanyNameInput) sdkgo.MutationAttempt[CompanyProfile] {
	if !validCompanyName(input.Name) {
		return keapMutationBranch("createCompany", CreateCompanyBranchDefect, CompanyProfile{}, sdkgo.FailureValidation, "bounded company name is required")
	}
	body, _ := json.Marshal(map[string]string{"company_name": input.Name})
	fence, err := singledispatch.NewFence(call, CreateCompanyDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return keapMutationBranch("createCompany", CreateCompanyBranchDefect, CompanyProfile{}, sdkgo.FailureLocalDefect, "company dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CompanyProfile{}, keapFailure("createCompany", sdkgo.FailureTransport, "an earlier company create may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestWithContext(requestContext, call, http.MethodPost, "/rest/v2/companies", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CompanyProfile](keapFailure("createCompany", sdkgo.FailureAvailability, "company dispatch could not start"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CompanyProfile{}, keapFailure("createCompany", sdkgo.FailureAuthentication, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch("createCompany", CreateCompanyBranchDefect, CompanyProfile{}, sdkgo.FailureAuthentication, "Keap credentials are unavailable")
	}
	if response.status == 429 || response.status == 503 {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CompanyProfile](keapFailure("createCompany", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[CompanyProfile](keapFailure("createCompany", sdkgo.FailureAvailability, "Keap did not process the create"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == 408 || response.status >= 500 || response.err != nil {
		failure := keapFailure("createCompany", sdkgo.FailureTransport, "company create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[CompanyProfile](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(CompanyProfile{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CompanyProfile{}, keapFailure("createCompany", sdkgo.FailureProviderRejection, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch("createCompany", CreateCompanyBranchProviderRejected, CompanyProfile{}, sdkgo.FailureProviderRejection, "Keap rejected company create")
	}
	company, ok := decodeCompany(response.body)
	if !ok {
		return sdkgo.NewMutationUncertain(CompanyProfile{}, keapFailure("createCompany", sdkgo.FailureProtocol, "created company identity cannot be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateCompanyBranchCreated, company, nil, sdkgo.Receipt{})
}

// Invoke replaces a company's name.
func (operation UpdateCompanyOperation) Invoke(call sdkgo.Call, input UpdateCompanyInput) sdkgo.MutationAttempt[CompanyProfile] {
	if !validContactID(input.CompanyID) || !validCompanyName(input.Name) {
		return keapMutationBranch("updateCompany", UpdateCompanyBranchDefect, CompanyProfile{}, sdkgo.FailureValidation, "company ID and bounded name are required")
	}
	body, _ := json.Marshal(map[string]string{"company_name": input.Name})
	response := operation.client.request(call, http.MethodPatch, "/rest/v2/companies/"+input.CompanyID, body)
	if response.status == 404 {
		return keapMutationBranch("updateCompany", UpdateCompanyBranchNotFound, CompanyProfile{}, sdkgo.FailureNotFound, "Keap did not find the company")
	}
	if branch, retry, handled := classifyKeapMutation(response, "updateCompany", UpdateCompanyBranchProviderRejected, UpdateCompanyBranchInvalidResponse, UpdateCompanyBranchDefect, CompanyProfile{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 {
		return keapMutationBranch("updateCompany", UpdateCompanyBranchProviderRejected, CompanyProfile{}, sdkgo.FailureProviderRejection, "Keap rejected company update")
	}
	company, ok := decodeCompany(response.body)
	if !ok {
		return keapMutationBranch("updateCompany", UpdateCompanyBranchInvalidResponse, CompanyProfile{}, sdkgo.FailureProtocol, "Keap returned invalid company JSON")
	}
	return sdkgo.NewMutationBranch(UpdateCompanyBranchUpdated, company, nil, sdkgo.Receipt{})
}

// Invoke deletes a company by ID.
func (operation DeleteCompanyOperation) Invoke(call sdkgo.Call, input CompanyIDInput) sdkgo.MutationAttempt[CompanyDeletion] {
	if !validContactID(input.CompanyID) {
		return keapMutationBranch("deleteCompany", DeleteCompanyBranchDefect, CompanyDeletion{}, sdkgo.FailureValidation, "company ID is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/rest/v2/companies/"+input.CompanyID, nil)
	if response.status == 404 {
		return keapMutationBranch("deleteCompany", DeleteCompanyBranchNotFound, CompanyDeletion{}, sdkgo.FailureNotFound, "Keap did not find the company")
	}
	if branch, retry, handled := classifyKeapMutation(response, "deleteCompany", DeleteCompanyBranchProviderRejected, DeleteCompanyBranchInvalidResponse, DeleteCompanyBranchDefect, CompanyDeletion{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 204 && response.status != 200 {
		return keapMutationBranch("deleteCompany", DeleteCompanyBranchProviderRejected, CompanyDeletion{}, sdkgo.FailureProviderRejection, "Keap rejected company deletion")
	}
	if response.status == 200 && len(response.body) > 0 && !json.Valid(response.body) {
		return keapMutationBranch("deleteCompany", DeleteCompanyBranchInvalidResponse, CompanyDeletion{}, sdkgo.FailureProtocol, "Keap returned invalid deletion JSON")
	}
	return sdkgo.NewMutationBranch(DeleteCompanyBranchDeleted, CompanyDeletion{CompanyID: input.CompanyID, Deleted: true}, nil, sdkgo.Receipt{})
}

func decodeCompany(body []byte) (CompanyProfile, bool) {
	var wire struct {
		ID   string `json:"id"`
		Name string `json:"company_name"`
	}
	if json.Unmarshal(body, &wire) != nil || !validContactID(wire.ID) {
		return CompanyProfile{}, false
	}
	return CompanyProfile{ID: wire.ID, Name: wire.Name}, true
}

func validCompanyName(name string) bool { return strings.TrimSpace(name) != "" && len(name) <= 256 }
