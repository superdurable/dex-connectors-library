// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gusto

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListEmployeesInput selects one bounded page from an authorized company.
type ListEmployeesInput struct {
	// CompanyID is the company UUID from Gusto's authorized company.
	CompanyID string `json:"companyId"`
	// Page is a positive one-based page number.
	Page int `json:"page"`
	// PerPage is the number of employees per page, from 1 to 100.
	PerPage int `json:"perPage"`
}

// EmployeesPage contains selected employee fields for one company page.
type EmployeesPage struct {
	// Employees excludes compensation, tax, address, and job details.
	Employees []Employee `json:"employees"`
}

// ListEmployeesOperation reads employees from one company.
type ListEmployeesOperation struct{ client *Client }

// ListEmployees binds the Query to this client.
func (client *Client) ListEmployees() ListEmployeesOperation { return ListEmployeesOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListEmployeesOperation) Definition() sdkgo.QueryDefinition { return ListEmployeesDefinition }

// Invoke reads one page and classifies retryable and definitive outcomes.
func (operation ListEmployeesOperation) Invoke(call sdkgo.Call, input ListEmployeesInput) sdkgo.QueryAttempt[EmployeesPage] {
	if !employeeUUIDPattern.MatchString(input.CompanyID) || input.Page < 1 || input.PerPage < 1 || input.PerPage > 100 {
		return employeesPageBranch(ListEmployeesBranchDefect, sdkgo.FailureValidation, "Gusto company UUID or page settings are invalid")
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per": {strconv.Itoa(input.PerPage)}}
	response := operation.client.get(call, "/v1/companies/"+input.CompanyID+"/employees?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return employeesPageBranch(ListEmployeesBranchDefect, sdkgo.FailureAuthentication, "Gusto OAuth credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[EmployeesPage](employeesPageFailure(sdkgo.FailureTransport, "Gusto request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[EmployeesPage](employeesPageFailure(sdkgo.FailureRateLimit, "Gusto rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[EmployeesPage](employeesPageFailure(sdkgo.FailureAvailability, "Gusto is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return employeesPageBranch(ListEmployeesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Gusto response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[EmployeesPage](employeesPageFailure(sdkgo.FailureTransport, "Gusto response was interrupted"), 0)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(ListEmployeesBranchMissing, EmployeesPage{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return employeesPageBranch(ListEmployeesBranchProviderRejected, sdkgo.FailureProviderRejection, "Gusto rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeEmployeesPage(response.body, input.CompanyID)
	if err != nil {
		return employeesPageBranch(ListEmployeesBranchInvalidResponse, sdkgo.FailureProtocol, "Gusto returned invalid employee page JSON")
	}
	return sdkgo.NewQueryBranch(ListEmployeesBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeEmployeesPage(body []byte, companyID string) (EmployeesPage, error) {
	var records []struct {
		UUID        string  `json:"uuid"`
		FirstName   string  `json:"first_name"`
		LastName    string  `json:"last_name"`
		Email       *string `json:"email"`
		CompanyUUID string  `json:"company_uuid"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil || len(records) > 100 {
		return EmployeesPage{}, errors.New("invalid employee list")
	}
	page := EmployeesPage{Employees: make([]Employee, 0, len(records))}
	for _, record := range records {
		if !employeeUUIDPattern.MatchString(record.UUID) || !strings.EqualFold(record.CompanyUUID, companyID) || strings.TrimSpace(record.FirstName) == "" || strings.TrimSpace(record.LastName) == "" {
			return EmployeesPage{}, errors.New("invalid employee identity")
		}
		employee := Employee{EmployeeID: record.UUID, FirstName: record.FirstName, LastName: record.LastName, CompanyID: record.CompanyUUID}
		if record.Email != nil {
			employee.Email = *record.Email
		}
		page.Employees = append(page.Employees, employee)
	}
	return page, nil
}

func employeesPageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "gusto", Operation: "listEmployees", Message: message}
}

func employeesPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[EmployeesPage] {
	failure := employeesPageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, EmployeesPage{}, &failure, sdkgo.Receipt{})
}
