// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateEmployeeOperation = "updateEmployee"

// UpdateEmployeeInput sets selected fields of one employee.
type UpdateEmployeeInput struct {
	// EmployeeID is BambooHR's internal employee ID, such as 123.
	EmployeeID string `json:"employeeId"`
	// Fields maps 1 to 50 field names or custom-field aliases, such as workEmail or
	// customITProvisioning, to the text to store; an empty value clears the field. Values use
	// BambooHR's field formats, such as YYYY-MM-DD for a date. Fields BambooHR keeps in its job
	// information, compensation, or employment status history tables, such as jobTitle,
	// department, location, payRate, employmentHistoryStatus, and terminationDate, are refused,
	// because writing them can append a history row and a repeated attempt could append another.
	// Numeric field IDs are refused for the same reason.
	Fields map[string]string `json:"fields"`
}

// UpdateEmployeeOutput is the employee after the change.
type UpdateEmployeeOutput struct {
	// Employee holds the requested fields as read back after the write, or as read when every
	// value was already applied.
	Employee EmployeeRecord `json:"employee"`
	// WrittenFields lists the fields this attempt sent, sorted; the others already held their value.
	WrittenFields []string `json:"writtenFields,omitempty"`
	// WasAlreadyApplied reports that the employee already held every requested value, for example
	// because an earlier attempt of this Step wrote them, so this attempt wrote nothing.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
	// UnmatchedFields lists requested fields whose stored value differs from the requested text
	// after the write, sorted: BambooHR rewrites some values, such as a state name to its
	// abbreviation, and silently drops a field the connection cannot see or edit.
	UnmatchedFields []string `json:"unmatchedFields,omitempty"`
}

// UpdateEmployeeOperation is the updateEmployee Mutation.
type UpdateEmployeeOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (UpdateEmployeeOperation) Definition() sdkgo.MutationDefinition { return UpdateEmployeeDefinition }

// IdempotencyKey uses the stable connector Call ID. BambooHR documents no idempotency key; the
// write is safe to repeat because it sets absolute plain values, so the key only correlates the Receipt.
func (UpdateEmployeeOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateEmployeeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the requested fields, sends POST /api/v1/employees/{id} with only the fields that
// differ, and reads them back. A retried or concurrently dispatched attempt sends the same
// absolute values, or finds them applied and writes nothing, so it never bumps the employee's
// last-changed time for a value it already holds.
func (operation UpdateEmployeeOperation) Invoke(call sdkgo.Call, input UpdateEmployeeInput) sdkgo.MutationAttempt[UpdateEmployeeOutput] {
	if err := validateUpdateEmployeeInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateEmployeeBranchDefect, UpdateEmployeeOutput{}, bambooHRFailurePointer(sdkgo.FailureValidation, updateEmployeeOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, updateEmployeeOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateEmployeeBranchDefect, UpdateEmployeeOutput{}, failure, sdkgo.Receipt{})
	}
	fieldNames := sortedFieldNames(input.Fields)
	receipt := operation.client.receipt(call, input.EmployeeID)
	current, attempt, isTerminal := operation.readFields(call, credentials, input.EmployeeID, fieldNames, receipt, UpdateEmployeeOutput{})
	if isTerminal {
		return attempt
	}
	writes := planEmployeeChange(input.Fields, current)
	if len(writes) == 0 {
		return sdkgo.NewMutationBranch(UpdateEmployeeBranchUpdated, UpdateEmployeeOutput{Employee: current, WasAlreadyApplied: true}, nil, receipt)
	}
	currentOutput := UpdateEmployeeOutput{Employee: current}
	writeResult := operation.client.exchange(call, credentials, updateEmployeeOperation, bambooHRRequest{
		method: http.MethodPost, path: "/employees/" + input.EmployeeID, payload: writes,
	})
	if attempt, isTerminal := updateEmployeeAttemptForExchange(writeResult, receipt, currentOutput); isTerminal {
		return attempt
	}
	output := UpdateEmployeeOutput{WrittenFields: sortedFieldNames(writes)}
	stored, attempt, isTerminal := operation.readFields(call, credentials, input.EmployeeID, fieldNames, receipt, output)
	if isTerminal {
		return attempt
	}
	output.Employee, output.UnmatchedFields = stored, unmatchedFields(input.Fields, stored)
	return sdkgo.NewMutationBranch(UpdateEmployeeBranchUpdated, output, nil, receipt)
}

// readFields reads the requested fields, or returns the terminal attempt for a failed read.
func (operation UpdateEmployeeOperation) readFields(call sdkgo.Call, credentials Credentials, employeeID string, fieldNames []string,
	receipt sdkgo.Receipt, value UpdateEmployeeOutput) (EmployeeRecord, sdkgo.MutationAttempt[UpdateEmployeeOutput], bool) {
	result := operation.client.exchange(call, credentials, updateEmployeeOperation, buildGetEmployeeRequest(employeeID, fieldNames, false))
	if attempt, isTerminal := updateEmployeeAttemptForExchange(result, receipt, value); isTerminal {
		return EmployeeRecord{}, attempt, true
	}
	record, err := decodeEmployeeRecord(result.response.body, employeeID, fieldNames)
	if err != nil {
		return EmployeeRecord{}, sdkgo.NewMutationBranch(UpdateEmployeeBranchInvalidResponse, value,
			bambooHRFailurePointer(sdkgo.FailureProtocol, updateEmployeeOperation, "BambooHR returned an invalid employee: "+err.Error()), receipt), true
	}
	return record, sdkgo.MutationAttempt[UpdateEmployeeOutput]{}, false
}

// updateEmployeeAttemptForExchange retries every unconfirmed outcome, because the write sets absolute values.
func updateEmployeeAttemptForExchange(result bambooHRExchange, receipt sdkgo.Receipt, value UpdateEmployeeOutput) (sdkgo.MutationAttempt[UpdateEmployeeOutput], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateEmployeeOutput]{}, false
	case exchangeRateLimited, exchangeLimitExceeded, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewMutationRetry[UpdateEmployeeOutput](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateEmployeeBranchNotFound, value, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateEmployeeBranchInvalidResponse, value, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateEmployeeBranchDefect, value, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(UpdateEmployeeBranchProviderRejected, value, &result.failure, receipt), true
	}
}

func validateUpdateEmployeeInput(input UpdateEmployeeInput) error {
	if err := validateEmployeeID("employeeId", input.EmployeeID); err != nil {
		return err
	}
	return validateWrittenFields("fields", input.Fields, 1, append(append([]string(nil), historyTableFields...), ignoredOnWriteFields...))
}

// planEmployeeChange keeps the fields whose current value differs or that the read did not return.
func planEmployeeChange(requested map[string]string, current EmployeeRecord) map[string]string {
	writes := map[string]string{}
	for field, value := range requested {
		if stored, isPresent := current.Value(field); !isPresent || stored != value {
			writes[field] = value
		}
	}
	return writes
}

func unmatchedFields(requested map[string]string, stored EmployeeRecord) []string {
	var unmatched []string
	for _, field := range sortedFieldNames(requested) {
		if value, isPresent := stored.Value(field); !isPresent || value != requested[field] {
			unmatched = append(unmatched, field)
		}
	}
	return unmatched
}
