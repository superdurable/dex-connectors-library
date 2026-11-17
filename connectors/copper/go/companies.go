// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CompanyMetadata contains the public identity and details of a Copper company.
type CompanyMetadata struct {
	// ID is the stable numeric company ID.
	ID int64 `json:"id"`
	// Name is the company's display name.
	Name string `json:"name"`
	// EmailDomain is the account-unique email domain, when returned.
	EmailDomain string `json:"emailDomain,omitempty"`
	// Details are the optional notes.
	Details string `json:"details,omitempty"`
}

// CreateCompanyInput supplies the new company's name and unique email domain.
type CreateCompanyInput struct {
	// Name is the required display name.
	Name string `json:"name"`
	// EmailDomain is required to check a previous send by a unique provider field.
	EmailDomain string `json:"emailDomain"`
}

// GetCompanyInput identifies a company by positive numeric ID.
type GetCompanyInput struct {
	// CompanyID is a positive decimal Copper company ID.
	CompanyID string `json:"companyId"`
}

// UpdateCompanyInput changes the named fields of one company.
type UpdateCompanyInput struct {
	// CompanyID is a positive numeric ID.
	CompanyID string `json:"companyId"`
	// Name replaces the display name.
	Name string `json:"name"`
	// Details replaces the notes; blank clears them.
	Details string `json:"details"`
}

// DeleteCompanyInput identifies the company to remove permanently.
type DeleteCompanyInput struct {
	// CompanyID is a positive decimal Copper company ID.
	CompanyID string `json:"companyId"`
}

// DeleteCompanyOutput confirms a deleted company ID.
type DeleteCompanyOutput struct {
	// ID is the deleted numeric ID.
	ID int64 `json:"id"`
	// Deleted is true when Copper confirms deletion.
	Deleted bool `json:"deleted"`
}

// CreateCompanyOperation creates a company with a bounded send count.
type CreateCompanyOperation struct{ client *Client }

// GetCompanyOperation reads a company by ID.
type GetCompanyOperation struct{ client *Client }

// UpdateCompanyOperation changes only the name and details.
type UpdateCompanyOperation struct{ client *Client }

// DeleteCompanyOperation removes a company permanently.
type DeleteCompanyOperation struct{ client *Client }

// CreateCompany binds the mutation to this client.
func (client *Client) CreateCompany() CreateCompanyOperation { return CreateCompanyOperation{client} }

// GetCompany binds the query to this client.
func (client *Client) GetCompany() GetCompanyOperation { return GetCompanyOperation{client} }

// UpdateCompany binds the mutation to this client.
func (client *Client) UpdateCompany() UpdateCompanyOperation { return UpdateCompanyOperation{client} }

// DeleteCompany binds the mutation to this client.
func (client *Client) DeleteCompany() DeleteCompanyOperation { return DeleteCompanyOperation{client} }

// Definition returns the generated mutation contract.
func (CreateCompanyOperation) Definition() sdkgo.MutationDefinition { return CreateCompanyDefinition }

// Definition returns the generated query contract.
func (GetCompanyOperation) Definition() sdkgo.QueryDefinition { return GetCompanyDefinition }

// Definition returns the generated mutation contract.
func (UpdateCompanyOperation) Definition() sdkgo.MutationDefinition { return UpdateCompanyDefinition }

// Definition returns the generated mutation contract.
func (DeleteCompanyOperation) Definition() sdkgo.MutationDefinition { return DeleteCompanyDefinition }

// IdempotencyKey identifies attempts; Copper does not deduplicate it.
func (CreateCompanyOperation) IdempotencyKey(id sdkgo.CallID, _ CreateCompanyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of an absolute field update.
func (UpdateCompanyOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateCompanyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of a deletion.
func (DeleteCompanyOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteCompanyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke reads one company without custom fields.
func (operation GetCompanyOperation) Invoke(call sdkgo.Call, input GetCompanyInput) sdkgo.QueryAttempt[CompanyMetadata] {
	branches := queryBranches{found: GetCompanyBranchFound, notFound: GetCompanyBranchNotFound, rejected: GetCompanyBranchProviderRejected, invalid: GetCompanyBranchInvalidResponse, defect: GetCompanyBranchDefect}
	if !validCompanyID(input.CompanyID) {
		return queryBranch("getCompany", branches.defect, CompanyMetadata{}, sdkgo.FailureValidation, "company ID must be a positive decimal identifier")
	}
	return invokeQuery(call, operation.client, "getCompany", "/companies/"+input.CompanyID, branches, decodeCompany)
}

// Invoke replaces one company's name and notes, leaving other provider fields intact.
func (operation UpdateCompanyOperation) Invoke(call sdkgo.Call, input UpdateCompanyInput) sdkgo.MutationAttempt[CompanyMetadata] {
	if !validCompanyID(input.CompanyID) || strings.TrimSpace(input.Name) == "" {
		return companyFailure[CompanyMetadata]("updateCompany", UpdateCompanyBranchDefect, sdkgo.FailureValidation, "company ID and name are required")
	}
	body, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Details string `json:"details"`
	}{input.Name, input.Details})
	response := operation.client.personRequest(call.Context, call, http.MethodPut, "/companies/"+input.CompanyID, body)
	if retry, failure := companyResponseFailure("updateCompany", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[CompanyMetadata](*failure, response.retryAfter)
		}
		branch := UpdateCompanyBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdateCompanyBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = UpdateCompanyBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, CompanyMetadata{}, failure, sdkgo.Receipt{})
	}
	company, err := decodeCompany(response.body)
	if response.status != http.StatusOK || err != nil {
		return sdkgo.NewMutationRetry[CompanyMetadata](queryFailure("updateCompany", sdkgo.FailureProtocol, "Copper update response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateCompanyBranchUpdated, company, nil, sdkgo.Receipt{})
}

// Invoke permanently removes one company.
func (operation DeleteCompanyOperation) Invoke(call sdkgo.Call, input DeleteCompanyInput) sdkgo.MutationAttempt[DeleteCompanyOutput] {
	if !validCompanyID(input.CompanyID) {
		return companyFailure[DeleteCompanyOutput]("deleteCompany", DeleteCompanyBranchDefect, sdkgo.FailureValidation, "company ID must be positive")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodDelete, "/companies/"+input.CompanyID, nil)
	if retry, failure := companyResponseFailure("deleteCompany", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[DeleteCompanyOutput](*failure, response.retryAfter)
		}
		branch := DeleteCompanyBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = DeleteCompanyBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = DeleteCompanyBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, DeleteCompanyOutput{}, failure, sdkgo.Receipt{})
	}
	var wire struct {
		ID      int64 `json:"id"`
		Deleted bool  `json:"is_deleted"`
	}
	if response.status != http.StatusOK || json.Unmarshal(response.body, &wire) != nil || wire.ID <= 0 || !wire.Deleted {
		return sdkgo.NewMutationRetry[DeleteCompanyOutput](queryFailure("deleteCompany", sdkgo.FailureProtocol, "Copper deletion response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteCompanyBranchDeleted, DeleteCompanyOutput{wire.ID, true}, nil, sdkgo.Receipt{})
}

// Invoke creates a company and checks its unique email domain before any resend.
func (operation CreateCompanyOperation) Invoke(call sdkgo.Call, input CreateCompanyInput) sdkgo.MutationAttempt[CompanyMetadata] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || !validCompanyDomain(input.EmailDomain) {
		return companyFailure[CompanyMetadata]("createCompany", CreateCompanyBranchDefect, sdkgo.FailureValidation, "name and unique email domain are required")
	}
	fence, err := singledispatch.NewFence(call, CreateCompanyDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return companyFailure[CompanyMetadata]("createCompany", CreateCompanyBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return companyUncertain("previous company creation cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed {
		found, isKnown, retry := operation.client.findCompanyByEmailDomain(call, input.EmailDomain, input.Name)
		if retry {
			return sdkgo.NewMutationRetry[CompanyMetadata](queryFailure("createCompany", sdkgo.FailureAvailability, "Copper domain search unavailable"), 0)
		}
		if found != nil {
			return sdkgo.NewMutationBranch(CreateCompanyBranchCreated, *found, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired || !isKnown {
			return companyUncertain("previous company creation cannot be confirmed")
		}
	}
	body, err := json.Marshal(struct {
		Name        string `json:"name"`
		EmailDomain string `json:"email_domain"`
	}{Name: input.Name, EmailDomain: input.EmailDomain})
	if err != nil {
		return companyFailure[CompanyMetadata]("createCompany", CreateCompanyBranchDefect, sdkgo.FailureLocalDefect, "company request could not be encoded")
	}
	var response personResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.personRequest(requestContext, call, http.MethodPost, "/companies", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CompanyMetadata](queryFailure("createCompany", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CompanyMetadata](queryFailure("createCompany", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[CompanyMetadata](queryFailure("createCompany", kind, "Copper did not create the company"), response.retryAfter)
	}
	if response.err != nil || response.status >= 500 {
		return sdkgo.NewMutationRetry[CompanyMetadata](queryFailure("createCompany", sdkgo.FailureTransport, "company creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return companyUncertain("an earlier company creation may have succeeded")
		}
		return companyFailure[CompanyMetadata]("createCompany", CreateCompanyBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	company, decodeErr := decodeCompany(response.body)
	if response.status != http.StatusOK || decodeErr != nil {
		return sdkgo.NewMutationRetry[CompanyMetadata](queryFailure("createCompany", sdkgo.FailureProtocol, "company creation response is invalid"), 0)
	}
	if err := fence.RecordProviderID(strconv.FormatInt(company.ID, 10)); err != nil {
		return sdkgo.NewMutationRetry[CompanyMetadata](queryFailure("createCompany", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateCompanyBranchCreated, company, nil, sdkgo.Receipt{})
}

func (client *Client) findCompanyByEmailDomain(call sdkgo.Call, domain string, name string) (*CompanyMetadata, bool, bool) {
	body, err := json.Marshal(struct {
		EmailDomains string `json:"email_domains"`
	}{domain})
	if err != nil {
		return nil, false, true
	}
	response := client.personRequest(call.Context, call, http.MethodPost, "/companies/search", body)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return nil, false, true
	}
	for _, record := range records {
		company, err := decodeCompany(record)
		if err != nil {
			return nil, false, true
		}
		if strings.EqualFold(company.EmailDomain, domain) {
			if company.Name == name {
				return &company, true, false
			}
			return nil, false, false
		}
	}
	return nil, true, false
}

func decodeCompany(body []byte) (CompanyMetadata, error) {
	var wire struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Details     string `json:"details"`
		EmailDomain string `json:"email_domain"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CompanyMetadata{}, err
	}
	if wire.ID <= 0 || strings.TrimSpace(wire.Name) == "" {
		return CompanyMetadata{}, errors.New("company identity missing")
	}
	return CompanyMetadata{ID: wire.ID, Name: wire.Name, Details: wire.Details, EmailDomain: wire.EmailDomain}, nil
}

func validCompanyID(id string) bool {
	value, err := strconv.ParseUint(id, 10, 63)
	return err == nil && value > 0 && strings.TrimSpace(id) == id
}

func validCompanyDomain(domain string) bool {
	return len(domain) <= 253 && strings.Contains(domain, ".") && !strings.ContainsAny(domain, "@ /\\\r\n\t") && strings.TrimSpace(domain) == domain
}

func companyResponseFailure(operation string, response personResponse) (bool, *sdkgo.Failure) {
	retry, failure := personResponseFailure(operation, response)
	if failure != nil {
		failure.Message = "Copper company request failed"
	}
	return retry, failure
}

func companyFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func companyUncertain(message string) sdkgo.MutationAttempt[CompanyMetadata] {
	return sdkgo.NewMutationUncertain(CompanyMetadata{}, queryFailure("createCompany", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
