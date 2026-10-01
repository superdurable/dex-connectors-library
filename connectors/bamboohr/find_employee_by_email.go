// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	findEmployeeByEmailOperation = "findEmployeeByEmail"

	// MaxEmailCandidates is the number of employees whose email contains the address that one call reads.
	MaxEmailCandidates = 100
	// MaxEmployeeSummaryFields bounds the additional fields findEmployeeByEmail returns per employee.
	MaxEmployeeSummaryFields = 20
)

// EmployeeEmailField is the BambooHR employee field an email lookup matches.
type EmployeeEmailField string

const (
	// EmployeeEmailFieldWork matches BambooHR's workEmail field.
	EmployeeEmailFieldWork EmployeeEmailField = "workEmail"
	// EmployeeEmailFieldHome matches BambooHR's homeEmail field, often the only address a new hire has.
	EmployeeEmailFieldHome EmployeeEmailField = "homeEmail"
)

// EmployeeStatusFilter is a List Employees status filter value, which BambooHR writes in lowercase.
type EmployeeStatusFilter string

const (
	// EmployeeStatusFilterActive keeps employees whose status is Active.
	EmployeeStatusFilterActive EmployeeStatusFilter = "active"
	// EmployeeStatusFilterInactive keeps employees whose status is Inactive.
	EmployeeStatusFilterInactive EmployeeStatusFilter = "inactive"
)

// summaryFields are the List Employees fields EmployeeSummary carries as typed values.
var summaryFields = []string{
	"employeeId", "firstName", "lastName", "preferredName", "jobTitleName", "status", "workEmail", "homeEmail",
	"photoUrl", "_restrictedFields",
}

// FindEmployeeByEmailInput is one email lookup.
type FindEmployeeByEmailInput struct {
	// Email is one bare address such as ava.nguyen@example.com. Letter case is ignored.
	Email string `json:"email"`
	// EmailField is the field to match, workEmail or homeEmail; blank matches workEmail.
	EmailField EmployeeEmailField `json:"emailField,omitempty"`
	// Status keeps only active or inactive employees; blank keeps both.
	Status EmployeeStatusFilter `json:"status,omitempty"`
	// Fields lists up to 20 additional List Employees field names to return per employee, such as
	// hireDate or departmentName. BambooHR ignores an unknown name.
	Fields []string `json:"fields,omitempty"`
}

// EmployeeSummary is one employee as BambooHR's List Employees endpoint returned it.
type EmployeeSummary struct {
	// EmployeeID is BambooHR's internal employee ID.
	EmployeeID string `json:"employeeId"`
	// FirstName is the legal first name.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the legal last name.
	LastName string `json:"lastName,omitempty"`
	// PreferredName is the preferred name, or empty.
	PreferredName string `json:"preferredName,omitempty"`
	// JobTitleName is the current job title.
	JobTitleName string `json:"jobTitleName,omitempty"`
	// Status is BambooHR's employee status, Active or Inactive.
	Status string `json:"status,omitempty"`
	// WorkEmail is the work email address, or empty.
	WorkEmail string `json:"workEmail,omitempty"`
	// HomeEmail is the home email address, or empty.
	HomeEmail string `json:"homeEmail,omitempty"`
	// Fields holds the additional requested fields BambooHR returned, as text.
	Fields map[string]string `json:"fields,omitempty"`
	// RestrictedFields lists fields BambooHR returned as null because the connection cannot read them.
	RestrictedFields []string `json:"restrictedFields,omitempty"`
}

// FindEmployeeByEmailOutput is the result of one email lookup.
type FindEmployeeByEmailOutput struct {
	// Employee is the one matching employee on the found branch.
	Employee EmployeeSummary `json:"employee"`
	// Matches lists every exact match read on the ambiguous branch, which may be fewer than two
	// when more than MaxEmailCandidates employee emails contain the address.
	Matches []EmployeeSummary `json:"matches,omitempty"`
	// CandidateCount is BambooHR's count of employees whose email contains the address.
	CandidateCount int `json:"candidateCount"`
}

// FindEmployeeByEmailOperation is the findEmployeeByEmail Query.
type FindEmployeeByEmailOperation struct {
	client *Client
}

type listEmployeesPageWire struct {
	Data []json.RawMessage `json:"data"`
	Meta *struct {
		Total *int `json:"total"`
		Page  struct {
			NextCursor *string `json:"nextCursor"`
		} `json:"page"`
	} `json:"meta"`
	Links struct {
		Next *struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
}

// Definition returns the immutable connector operation definition.
func (FindEmployeeByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindEmployeeByEmailDefinition
}

// Invoke reads one page of GET /api/v1/employees?filter[workEmail]=..., BambooHR's documented
// case-insensitive substring filter, sorted by employee ID with page[limit]=100, then keeps the
// employees whose email equals the address.
func (operation FindEmployeeByEmailOperation) Invoke(call sdkgo.Call, input FindEmployeeByEmailInput) sdkgo.QueryAttempt[FindEmployeeByEmailOutput] {
	input.EmailField = emailFieldOrDefault(input.EmailField)
	if err := validateFindEmployeeByEmailInput(input); err != nil {
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchDefect, FindEmployeeByEmailOutput{}, bambooHRFailurePointer(sdkgo.FailureValidation, findEmployeeByEmailOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, findEmployeeByEmailOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchDefect, FindEmployeeByEmailOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, findEmployeeByEmailOperation, buildFindEmployeeByEmailRequest(input))
	receipt := operation.client.receipt(call, "")
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeLimitExceeded, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[FindEmployeeByEmailOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchInvalidResponse, FindEmployeeByEmailOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchDefect, FindEmployeeByEmailOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchProviderRejected, FindEmployeeByEmailOutput{}, &result.failure, receipt)
	}
	candidates, candidateCount, hasMoreCandidates, err := decodeEmployeeCandidates(result.response.body, input.Fields)
	if err != nil {
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchInvalidResponse, FindEmployeeByEmailOutput{},
			bambooHRFailurePointer(sdkgo.FailureProtocol, findEmployeeByEmailOperation, "BambooHR returned an invalid employee list: "+err.Error()), receipt)
	}
	matches := exactEmailMatches(candidates, input.EmailField, input.Email)
	output := FindEmployeeByEmailOutput{CandidateCount: candidateCount}
	switch {
	case len(matches) == 1 && !hasMoreCandidates:
		output.Employee = matches[0]
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchFound, output, nil, operation.client.receipt(call, matches[0].EmployeeID))
	case len(matches) == 0 && !hasMoreCandidates:
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchNotFound, output,
			bambooHRFailurePointer(sdkgo.FailureNotFound, findEmployeeByEmailOperation, "no employee visible to the connection has this "+string(input.EmailField)), receipt)
	default:
		output.Matches = matches
		return sdkgo.NewQueryBranch(FindEmployeeByEmailBranchAmbiguous, output, bambooHRFailurePointer(sdkgo.FailureConflict, findEmployeeByEmailOperation,
			fmt.Sprintf("exact matches: %d of %d employees whose %s contains the address; no single employee was chosen", len(matches), candidateCount, input.EmailField)), receipt)
	}
}

func emailFieldOrDefault(field EmployeeEmailField) EmployeeEmailField {
	if field == "" {
		return EmployeeEmailFieldWork
	}
	return field
}

func validateFindEmployeeByEmailInput(input FindEmployeeByEmailInput) error {
	if !isBareEmailAddress(input.Email) {
		return errors.New("email must be one bare email address such as ava.nguyen@example.com")
	}
	if input.EmailField != EmployeeEmailFieldWork && input.EmailField != EmployeeEmailFieldHome {
		return errors.New("emailField must be workEmail or homeEmail")
	}
	if input.Status != "" && input.Status != EmployeeStatusFilterActive && input.Status != EmployeeStatusFilterInactive {
		return errors.New("status must be active, inactive, or blank")
	}
	if len(input.Fields) > MaxEmployeeSummaryFields {
		return fmt.Errorf("fields can name at most %d additional fields", MaxEmployeeSummaryFields)
	}
	for _, field := range input.Fields {
		if !fieldNamePattern.MatchString(field) {
			return fmt.Errorf("fields entry %q must be a BambooHR List Employees field name", field)
		}
	}
	return nil
}

func buildFindEmployeeByEmailRequest(input FindEmployeeByEmailInput) bambooHRRequest {
	fields := []string{string(EmployeeEmailFieldWork), string(EmployeeEmailFieldHome)}
	for _, field := range input.Fields {
		if !slices.Contains(fields, field) && !slices.Contains(summaryFields, field) {
			fields = append(fields, field)
		}
	}
	query := url.Values{
		"filter[" + string(input.EmailField) + "]": {input.Email},
		"fields":      {strings.Join(fields, ",")},
		"sort":        {"employeeId"},
		"page[limit]": {strconv.Itoa(MaxEmailCandidates)},
	}
	if input.Status != "" {
		query.Set("filter[status]", string(input.Status))
	}
	return bambooHRRequest{method: http.MethodGet, path: "/employees", query: query}
}

// decodeEmployeeCandidates returns the page's employees, BambooHR's total, and whether more candidates exist.
func decodeEmployeeCandidates(body []byte, requestedFields []string) ([]EmployeeSummary, int, bool, error) {
	var page listEmployeesPageWire
	if err := json.Unmarshal(body, &page); err != nil || page.Data == nil || page.Meta == nil || page.Meta.Total == nil {
		return nil, 0, false, errors.New("the response is not a List Employees page")
	}
	if len(page.Data) > MaxEmailCandidates || *page.Meta.Total < len(page.Data) {
		return nil, 0, false, errors.New("the page size or total is inconsistent")
	}
	candidates := make([]EmployeeSummary, 0, len(page.Data))
	for index, raw := range page.Data {
		candidate, err := decodeEmployeeSummary(raw, requestedFields)
		if err != nil {
			return nil, 0, false, fmt.Errorf("employee %d: %w", index, err)
		}
		candidates = append(candidates, candidate)
	}
	hasNextCursor := page.Meta.Page.NextCursor != nil && *page.Meta.Page.NextCursor != ""
	hasMore := hasNextCursor || page.Links.Next != nil || *page.Meta.Total > len(page.Data)
	return candidates, *page.Meta.Total, hasMore, nil
}

func decodeEmployeeSummary(raw json.RawMessage, requestedFields []string) (EmployeeSummary, error) {
	var restricted struct {
		RestrictedFields []string `json:"_restrictedFields"`
	}
	if err := json.Unmarshal(raw, &restricted); err != nil {
		return EmployeeSummary{}, errors.New("_restrictedFields is not a list of names")
	}
	employeeID, fields, err := decodeEmployeeObject(raw, "employeeId")
	if err != nil {
		return EmployeeSummary{}, err
	}
	summary := EmployeeSummary{
		EmployeeID: employeeID, FirstName: fields["firstName"], LastName: fields["lastName"], PreferredName: fields["preferredName"],
		JobTitleName: fields["jobTitleName"], Status: fields["status"], WorkEmail: fields["workEmail"], HomeEmail: fields["homeEmail"],
		RestrictedFields: restricted.RestrictedFields,
	}
	for _, field := range requestedFields {
		if value, isPresent := fields[field]; isPresent && !slices.Contains(summaryFields, field) {
			if summary.Fields == nil {
				summary.Fields = map[string]string{}
			}
			summary.Fields[field] = value
		}
	}
	return summary, nil
}

// exactEmailMatches keeps the candidates whose email field equals the address, ignoring letter case.
func exactEmailMatches(candidates []EmployeeSummary, field EmployeeEmailField, email string) []EmployeeSummary {
	var matches []EmployeeSummary
	for _, candidate := range candidates {
		value := candidate.WorkEmail
		if field == EmployeeEmailFieldHome {
			value = candidate.HomeEmail
		}
		if strings.EqualFold(strings.TrimSpace(value), email) {
			matches = append(matches, candidate)
		}
	}
	return matches
}
