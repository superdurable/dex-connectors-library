// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	addEmployeeOperation = "addEmployee"

	maxNameBytes = 255
)

var (
	// addEmployeeTypedFields are the AddEmployeeInput fields that Fields cannot repeat.
	addEmployeeTypedFields    = []string{"firstName", "lastName", "preferredName", "workEmail", "homeEmail", "hireDate"}
	locationEmployeeIDPattern = regexp.MustCompile(`(?:/employees/|[?&]id=)([1-9][0-9]{0,18})(?:[/?&#]|$)`)
)

// AddEmployeeInput is one new employee. BambooHR requires only the first and last name.
type AddEmployeeInput struct {
	// FirstName is the legal first name. It is required.
	FirstName string `json:"firstName"`
	// LastName is the legal last name. It is required.
	LastName string `json:"lastName"`
	// PreferredName is the preferred name, or empty for none.
	PreferredName string `json:"preferredName,omitempty"`
	// WorkEmail is a bare work email address, or empty when IT has not issued one yet. BambooHR
	// rejects an email another employee already holds.
	WorkEmail string `json:"workEmail,omitempty"`
	// HomeEmail is a bare personal email address, or empty.
	HomeEmail string `json:"homeEmail,omitempty"`
	// HireDate is the hire date written YYYY-MM-DD, or empty.
	HireDate string `json:"hireDate,omitempty"`
	// Fields maps up to 50 other writable field names or custom-field aliases to their text, such
	// as department or employmentHistoryStatus, which sets the first employment status record.
	// It cannot repeat a field above, name a numeric field ID, or set id or a photo.
	Fields map[string]string `json:"fields,omitempty"`
}

// AddEmployeeOutput is the employee BambooHR added.
type AddEmployeeOutput struct {
	// EmployeeID is the new employee's internal employee ID.
	EmployeeID string `json:"employeeId"`
}

// AddEmployeeOperation is the addEmployee Mutation.
type AddEmployeeOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (AddEmployeeOperation) Definition() sdkgo.MutationDefinition { return AddEmployeeDefinition }

// IdempotencyKey uses the stable connector Call ID. BambooHR documents no idempotency key, so the
// key only correlates the Receipt; single dispatch comes from a Dex heartbeat checkpoint instead.
func (AddEmployeeOperation) IdempotencyKey(callID sdkgo.CallID, _ AddEmployeeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /api/v1/employees at most once per Step execution. Only a rate-limited 429 or
// a connection that never opened is retried; any other unconfirmed outcome selects uncertain
// without resending.
func (operation AddEmployeeOperation) Invoke(call sdkgo.Call, input AddEmployeeInput) sdkgo.MutationAttempt[AddEmployeeOutput] {
	if err := validateAddEmployeeInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddEmployeeBranchDefect, AddEmployeeOutput{}, bambooHRFailurePointer(sdkgo.FailureValidation, addEmployeeOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, addEmployeeOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(AddEmployeeBranchDefect, AddEmployeeOutput{}, failure, sdkgo.Receipt{})
	}
	receipt := operation.client.receipt(call, "")
	switch claimSingleDispatch(call) {
	case singleDispatchGranted:
	case singleDispatchNotRecorded:
		return sdkgo.NewMutationRetry[AddEmployeeOutput](bambooHRFailure(sdkgo.FailureAvailability, addEmployeeOperation, "Dex did not record the dispatch checkpoint; nothing was sent"), 0)
	default:
		return sdkgo.NewMutationUncertain(AddEmployeeOutput{}, bambooHRFailure(sdkgo.FailureTransport, addEmployeeOperation,
			"an earlier attempt of this Step may have sent the request, so it is not sent again"), receipt)
	}
	result := operation.client.exchange(call, credentials, addEmployeeOperation, bambooHRRequest{
		method: http.MethodPost, path: "/employees", payload: buildAddEmployeeRequest(input),
	})
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeNotSent:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationRetry[AddEmployeeOutput](result.failure, result.retryAfter)
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationUncertain(AddEmployeeOutput{}, result.failure, receipt)
	case exchangeDefect:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationBranch(AddEmployeeBranchDefect, AddEmployeeOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(AddEmployeeBranchProviderRejected, AddEmployeeOutput{}, &result.failure, receipt)
	}
	employeeID, err := addedEmployeeID(result.response)
	if err != nil {
		return sdkgo.NewMutationUncertain(AddEmployeeOutput{}, bambooHRFailure(sdkgo.FailureProtocol, addEmployeeOperation,
			"BambooHR accepted the employee but "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(AddEmployeeBranchCreated, AddEmployeeOutput{EmployeeID: employeeID}, nil, operation.client.receipt(call, employeeID))
}

func validateAddEmployeeInput(input AddEmployeeInput) error {
	if err := errors.Join(validatePersonName("firstName", input.FirstName, true), validatePersonName("lastName", input.LastName, true),
		validatePersonName("preferredName", input.PreferredName, false)); err != nil {
		return err
	}
	for name, email := range map[string]string{"workEmail": input.WorkEmail, "homeEmail": input.HomeEmail} {
		if email != "" && !isBareEmailAddress(email) {
			return fmt.Errorf("%s must be one bare email address such as ava.nguyen@example.com", name)
		}
	}
	if input.HireDate != "" {
		if err := validateCivilDate("hireDate", input.HireDate); err != nil {
			return err
		}
	}
	return validateWrittenFields("fields", input.Fields, 0, append(append([]string(nil), addEmployeeTypedFields...), ignoredOnWriteFields...))
}

func validatePersonName(name string, value string, isRequired bool) error {
	switch {
	case value == "" && isRequired:
		return fmt.Errorf("%s is required", name)
	case value == "":
		return nil
	case len(value) > maxNameBytes || !utf8.ValidString(value) || value != strings.TrimSpace(value) ||
		strings.ContainsFunc(value, unicode.IsControl):
		return fmt.Errorf("%s must be at most %d bytes of text without surrounding spaces or control characters", name, maxNameBytes)
	}
	return nil
}

func buildAddEmployeeRequest(input AddEmployeeInput) map[string]string {
	payload := make(map[string]string, len(input.Fields)+len(addEmployeeTypedFields))
	for field, value := range input.Fields {
		payload[field] = value
	}
	payload["firstName"], payload["lastName"] = input.FirstName, input.LastName
	for field, value := range map[string]string{
		"preferredName": input.PreferredName, "workEmail": input.WorkEmail, "homeEmail": input.HomeEmail, "hireDate": input.HireDate,
	} {
		if value != "" {
			payload[field] = value
		}
	}
	return payload
}

// addedEmployeeID reads the new ID from the body's id, or from the Location header BambooHR documents.
func addedEmployeeID(response bambooHRResponse) (string, error) {
	var document map[string]json.RawMessage
	if json.Unmarshal(response.body, &document) == nil {
		if employeeID, err := decodeEmployeeIDValue(document["id"]); err == nil {
			return employeeID, nil
		}
	}
	if match := locationEmployeeIDPattern.FindStringSubmatch(response.header.Get("Location")); match != nil {
		return match[1], nil
	}
	return "", errors.New("returned no employee ID")
}
