// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getEmployeeOperation = "getEmployee"

// GetEmployeeInput names one employee and the fields to read.
type GetEmployeeInput struct {
	// EmployeeID is BambooHR's internal employee ID, such as 123. The editable Employee # and the
	// caller sentinel 0 are not accepted.
	EmployeeID string `json:"employeeId"`
	// Fields lists 1 to 400 fields to read: standard names such as workEmail, custom-field
	// aliases such as customStartDate, or numeric field IDs from BambooHR's List Fields endpoint.
	// BambooHR returns nothing but the ID when no field is requested, so at least one is required.
	Fields []string `json:"fields"`
	// IncludeFutureValues also returns future-dated values from history tables such as job
	// information, BambooHR's onlyCurrent=false. False reads only currently effective values.
	IncludeFutureValues bool `json:"includeFutureValues,omitempty"`
}

// GetEmployeeOperation is the getEmployee Query.
type GetEmployeeOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetEmployeeOperation) Definition() sdkgo.QueryDefinition { return GetEmployeeDefinition }

// Invoke reads GET /api/v1/employees/{id}?fields=... with the fields comma-separated, the only
// form BambooHR accepts.
func (operation GetEmployeeOperation) Invoke(call sdkgo.Call, input GetEmployeeInput) sdkgo.QueryAttempt[EmployeeRecord] {
	if err := validateGetEmployeeInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetEmployeeBranchDefect, EmployeeRecord{}, bambooHRFailurePointer(sdkgo.FailureValidation, getEmployeeOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, getEmployeeOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetEmployeeBranchDefect, EmployeeRecord{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, getEmployeeOperation,
		buildGetEmployeeRequest(input.EmployeeID, input.Fields, input.IncludeFutureValues))
	receipt := operation.client.receipt(call, input.EmployeeID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeLimitExceeded, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[EmployeeRecord](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetEmployeeBranchNotFound, EmployeeRecord{}, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetEmployeeBranchInvalidResponse, EmployeeRecord{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetEmployeeBranchDefect, EmployeeRecord{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetEmployeeBranchProviderRejected, EmployeeRecord{}, &result.failure, receipt)
	}
	record, err := decodeEmployeeRecord(result.response.body, input.EmployeeID, input.Fields)
	if err != nil {
		return sdkgo.NewQueryBranch(GetEmployeeBranchInvalidResponse, EmployeeRecord{},
			bambooHRFailurePointer(sdkgo.FailureProtocol, getEmployeeOperation, "BambooHR returned an invalid employee: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetEmployeeBranchFound, record, nil, receipt)
}

func validateGetEmployeeInput(input GetEmployeeInput) error {
	if err := validateEmployeeID("employeeId", input.EmployeeID); err != nil {
		return err
	}
	return validateRequestedFields("fields", input.Fields, 1, MaxRequestedFields)
}

func buildGetEmployeeRequest(employeeID string, fields []string, includeFutureValues bool) bambooHRRequest {
	query := url.Values{"fields": {strings.Join(fields, ",")}}
	if includeFutureValues {
		query.Set("onlyCurrent", "false")
	}
	return bambooHRRequest{method: http.MethodGet, path: "/employees/" + employeeID, query: query}
}

// decodeEmployeeRecord requires the returned ID to be the requested one.
func decodeEmployeeRecord(body []byte, employeeID string, requestedFields []string) (EmployeeRecord, error) {
	returnedID, fields, err := decodeEmployeeObject(body, "id")
	if err != nil {
		return EmployeeRecord{}, err
	}
	if returnedID != employeeID {
		return EmployeeRecord{}, errEmployeeIDMismatch
	}
	return EmployeeRecord{EmployeeID: returnedID, Fields: fields, OmittedFields: omittedFields(requestedFields, fields)}, nil
}
