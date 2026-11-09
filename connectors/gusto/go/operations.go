// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gusto

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var employeeUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// GetEmployeeInput identifies one employee in the authorized company.
type GetEmployeeInput struct {
	// EmployeeID is the employee UUID from Gusto's employee list or webhook.
	EmployeeID string `json:"employeeId"`
}

// Employee projects safe identity and contact fields from one Gusto employee.
type Employee struct {
	// EmployeeID is the stable Gusto employee UUID.
	EmployeeID string `json:"employeeId"`
	// FirstName is the employee's given name.
	FirstName string `json:"firstName"`
	// LastName is the employee's family name.
	LastName string `json:"lastName"`
	// Email is the personal email when Gusto supplies one; blank means absent.
	Email string `json:"email"`
	// CompanyID is the employing company's UUID.
	CompanyID string `json:"companyId"`
}

// GetEmployeeOperation reads one Gusto employee.
type GetEmployeeOperation struct{ client *Client }

// GetEmployee binds the Query to this client.
func (client *Client) GetEmployee() GetEmployeeOperation { return GetEmployeeOperation{client} }

// Definition returns the generated employee Query contract.
func (GetEmployeeOperation) Definition() sdkgo.QueryDefinition { return GetEmployeeDefinition }

// Invoke reads one employee and classifies the provider outcome.
func (operation GetEmployeeOperation) Invoke(call sdkgo.Call, input GetEmployeeInput) sdkgo.QueryAttempt[Employee] {
	if !employeeUUIDPattern.MatchString(input.EmployeeID) {
		return employeeBranch(GetEmployeeBranchDefect, sdkgo.FailureValidation, "Gusto employee UUID is invalid")
	}
	response := operation.client.get(call, "/v1/employees/"+input.EmployeeID)
	if errors.Is(response.err, errCredential) {
		return employeeBranch(GetEmployeeBranchDefect, sdkgo.FailureAuthentication, "Gusto OAuth credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return employeeRetry(sdkgo.FailureTransport, "Gusto request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return employeeRetry(sdkgo.FailureRateLimit, "Gusto rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return employeeRetry(sdkgo.FailureAvailability, "Gusto is temporarily unavailable", response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return employeeBranch(GetEmployeeBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Gusto response exceeded the configured limit")
	}
	if response.err != nil {
		return employeeRetry(sdkgo.FailureTransport, "Gusto response was interrupted", 0)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetEmployeeBranchMissing, Employee{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return employeeBranch(GetEmployeeBranchProviderRejected, sdkgo.FailureProviderRejection, "Gusto rejected HTTP "+strconv.Itoa(response.status))
	}
	employee, err := decodeEmployee(response.body, input)
	if err != nil {
		return employeeBranch(GetEmployeeBranchInvalidResponse, sdkgo.FailureProtocol, "Gusto returned invalid employee JSON")
	}
	return sdkgo.NewQueryBranch(GetEmployeeBranchFound, employee, nil, sdkgo.Receipt{})
}

func decodeEmployee(body []byte, input GetEmployeeInput) (Employee, error) {
	var record struct {
		UUID        string  `json:"uuid"`
		FirstName   string  `json:"first_name"`
		LastName    string  `json:"last_name"`
		Email       *string `json:"email"`
		CompanyUUID string  `json:"company_uuid"`
	}
	if err := json.Unmarshal(body, &record); err != nil {
		return Employee{}, err
	}
	if !strings.EqualFold(record.UUID, input.EmployeeID) || !employeeUUIDPattern.MatchString(record.UUID) ||
		!employeeUUIDPattern.MatchString(record.CompanyUUID) || strings.TrimSpace(record.FirstName) == "" ||
		strings.TrimSpace(record.LastName) == "" {
		return Employee{}, errors.New("Gusto employee fields are invalid")
	}
	email := ""
	if record.Email != nil {
		email = *record.Email
	}
	return Employee{EmployeeID: record.UUID, FirstName: record.FirstName, LastName: record.LastName, Email: email, CompanyID: record.CompanyUUID}, nil
}

func employeeFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "gusto", Operation: "getEmployee", Message: message}
}
func employeeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Employee] {
	failure := employeeFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, Employee{}, &failure, sdkgo.Receipt{})
}
func employeeRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[Employee] {
	return sdkgo.NewQueryRetry[Employee](employeeFailure(kind, message), delay)
}
