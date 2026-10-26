// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
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

type addEmployeeSendAttempt struct {
	client      *Client
	credentials Credentials
	input       AddEmployeeInput
	result      bambooHRExchange
}

func (attempt *addEmployeeSendAttempt) send(ctx context.Context) error {
	attempt.result = attempt.client.exchangeWithContext(ctx, attempt.credentials, addEmployeeOperation, bambooHRRequest{
		method: http.MethodPost, path: "/employees", payload: buildAddEmployeeRequest(attempt.input),
	}, true)
	return nil
}

// Definition returns the immutable connector operation definition.
func (AddEmployeeOperation) Definition() sdkgo.MutationDefinition { return AddEmployeeDefinition }

// IdempotencyKey uses the stable connector Call ID. BambooHR documents no idempotency key, so the
// key only correlates the Receipt; the shared single-dispatch fence owns the retry checkpoint.
func (AddEmployeeOperation) IdempotencyKey(callID sdkgo.CallID, _ AddEmployeeInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /api/v1/employees only when the shared fence allows it.
func (operation AddEmployeeOperation) Invoke(call sdkgo.Call, input AddEmployeeInput) sdkgo.MutationAttempt[AddEmployeeOutput] {
	if err := validateAddEmployeeInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddEmployeeBranchDefect, AddEmployeeOutput{}, bambooHRFailurePointer(sdkgo.FailureValidation, addEmployeeOperation, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, AddEmployeeDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(AddEmployeeBranchDefect, AddEmployeeOutput{}, bambooHRFailurePointer(sdkgo.FailureLocalDefect, addEmployeeOperation, err.Error()), sdkgo.Receipt{})
	}
	receipt := operation.client.receipt(call, "")
	switch decision := fence.Decide(); decision.Kind {
	case singledispatch.FailClosed:
		return addEmployeeUncertain(receipt, sdkgo.FailureTransport, "the dispatch checkpoint is unavailable or untrusted; the request is not sent again")
	case singledispatch.ReadBackRequired:
		if decision.Marker.CallID != call.ID || decision.Marker.ProviderID == "" {
			return addEmployeeUncertain(receipt, sdkgo.FailureTransport, "an earlier attempt may have sent the request; no employee ID is available for read-back")
		}
		return operation.readBackAddedEmployee(call, decision.Marker.ProviderID)
	}
	credentials, credentialErr := operation.client.credentials.Resolve(call)
	isUnusableCredentials := credentialErr == nil && validateResolvedCredentials(credentials) != nil
	if credentialErr != nil || isUnusableCredentials {
		if err := fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[AddEmployeeOutput](bambooHRFailure(sdkgo.FailureAvailability, addEmployeeOperation, "credentials and not-sent checkpoint are unavailable"), 0)
		}
		failure := bambooHRFailure(sdkgo.FailureAuthentication, addEmployeeOperation, "connection credentials are unavailable")
		if isTerminalAddEmployeeCredentialError(credentialErr) || isUnusableCredentials {
			return sdkgo.NewMutationBranch(AddEmployeeBranchDefect, AddEmployeeOutput{}, &failure, receipt)
		}
		return sdkgo.NewMutationRetry[AddEmployeeOutput](bambooHRFailure(sdkgo.FailureAvailability, addEmployeeOperation, "connection credentials could not be read"), 0)
	}
	attempt := &addEmployeeSendAttempt{client: operation.client, credentials: credentials, input: input}
	if err := fence.Dispatch(attempt.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[AddEmployeeOutput](bambooHRFailure(sdkgo.FailureAvailability, addEmployeeOperation, "dispatch checkpoint or attempt time is unavailable; nothing was sent"), 0)
		}
		return addEmployeeUncertain(receipt, sdkgo.FailureTransport, "the dispatch outcome is unavailable")
	}
	result := attempt.result
	if result.response.statusCode >= 200 && result.response.statusCode < 300 {
		employeeID, err := addedEmployeeID(result.response)
		if err != nil {
			return addEmployeeUncertain(receipt, sdkgo.FailureProtocol, "BambooHR accepted the employee but returned no usable employee ID")
		}
		// A failed identity checkpoint cannot authorize another POST.
		_ = fence.RecordProviderID(employeeID)
		return sdkgo.NewMutationBranch(AddEmployeeBranchCreated, AddEmployeeOutput{EmployeeID: employeeID}, nil, operation.client.receipt(call, employeeID))
	}
	if result.response.statusCode == http.StatusTooManyRequests || result.response.statusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AddEmployeeOutput](bambooHRFailure(sdkgo.FailureAvailability, addEmployeeOperation, "BambooHR did not process the request, but the resend checkpoint failed"), result.retryAfter)
		}
		return sdkgo.NewMutationRetry[AddEmployeeOutput](result.failure, result.retryAfter)
	}
	switch result.outcome {
	case exchangeNotSent:
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AddEmployeeOutput](bambooHRFailure(sdkgo.FailureAvailability, addEmployeeOperation, "BambooHR did not process the request, but the resend checkpoint failed"), result.retryAfter)
		}
		return sdkgo.NewMutationRetry[AddEmployeeOutput](result.failure, result.retryAfter)
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationUncertain(AddEmployeeOutput{}, result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(AddEmployeeBranchDefect, AddEmployeeOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(AddEmployeeBranchProviderRejected, AddEmployeeOutput{}, &result.failure, receipt)
	}
}

func (operation AddEmployeeOperation) readBackAddedEmployee(call sdkgo.Call, employeeID string) sdkgo.MutationAttempt[AddEmployeeOutput] {
	if validateEmployeeID("employeeId", employeeID) != nil {
		return addEmployeeUncertain(operation.client.receipt(call, ""), sdkgo.FailureProtocol, "the saved employee ID is invalid; no read-back was attempted")
	}
	receipt := operation.client.receipt(call, employeeID)
	credentials, failure := operation.client.resolveCredentials(call, addEmployeeOperation)
	if failure != nil {
		return sdkgo.NewMutationRetry[AddEmployeeOutput](*failure, 0)
	}
	result := operation.client.exchange(call, credentials, addEmployeeOperation, bambooHRRequest{
		method: http.MethodGet, path: "/employees/" + employeeID,
	})
	if result.outcome == exchangeSucceeded {
		var record struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(result.response.body, &record) == nil && record.ID == employeeID {
			return sdkgo.NewMutationBranch(AddEmployeeBranchCreated, AddEmployeeOutput{EmployeeID: employeeID}, nil, receipt)
		}
		return addEmployeeUncertain(receipt, sdkgo.FailureProtocol, "the created employee ID could not be confirmed")
	}
	if result.outcome == exchangeNotFound || result.outcome == exchangeRejected {
		return addEmployeeUncertain(receipt, result.failure.Kind, "the created employee could not be confirmed")
	}
	return sdkgo.NewMutationRetry[AddEmployeeOutput](result.failure, result.retryAfter)
}

func addEmployeeUncertain(receipt sdkgo.Receipt, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AddEmployeeOutput] {
	return sdkgo.NewMutationUncertain(AddEmployeeOutput{}, bambooHRFailure(kind, addEmployeeOperation, message), receipt)
}

func isTerminalAddEmployeeCredentialError(err error) bool {
	return errors.Is(err, sdkgo.ErrReauthorizationRequired) || errors.Is(err, projectconfig.ErrObjectNotFound) ||
		errors.Is(err, projectconfig.ErrConnectionNotSelected) || errors.Is(err, projectconfig.ErrCredentialBrokerRejected)
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
