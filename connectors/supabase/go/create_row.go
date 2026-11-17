// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supabase

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateRowInput inserts one row with a database-enforced unique string key.
type CreateRowInput struct {
	// Table is an exposed table name.
	Table string `json:"table"`
	// UniqueColumn has a database-enforced unique constraint and is present in Values.
	UniqueColumn string `json:"uniqueColumn"`
	// UniqueValue is this request's unique marker, distinct from other inserts.
	UniqueValue string `json:"uniqueValue"`
	// Values are the columns to insert, including UniqueColumn and UniqueValue.
	Values map[string]any `json:"values"`
}

// CreateRowOutput holds the inserted or reconciled row.
type CreateRowOutput struct {
	// Data is the JSON row object.
	Data json.RawMessage `json:"data"`
}

// CreateRowOperation inserts a row with bounded resend and unique-key read-back.
type CreateRowOperation struct{ client *Client }

// CreateRow returns this client's typed row mutation.
func (client *Client) CreateRow() CreateRowOperation { return CreateRowOperation{client} }

// Definition returns the generated operation contract.
func (CreateRowOperation) Definition() sdkgo.MutationDefinition { return CreateRowDefinition }

// IdempotencyKey keeps the call identity stable across retries; PostgREST does not deduplicate it.
func (CreateRowOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke inserts once per attempt and reconciles prior attempts by the unique marker.
func (operation CreateRowOperation) Invoke(call sdkgo.Call, input CreateRowInput) sdkgo.MutationAttempt[CreateRowOutput] {
	if !identifier.MatchString(input.Table) || !identifier.MatchString(input.UniqueColumn) || input.UniqueValue == "" || len(input.Values) == 0 {
		return createRowFailure(CreateRowBranchDefect, sdkgo.FailureValidation, "table, unique column, unique value, and row values are required")
	}
	for column := range input.Values {
		if !identifier.MatchString(column) {
			return createRowFailure(CreateRowBranchDefect, sdkgo.FailureValidation, "row column is invalid")
		}
	}
	if value, ok := input.Values[input.UniqueColumn].(string); !ok || value != input.UniqueValue {
		return createRowFailure(CreateRowBranchDefect, sdkgo.FailureValidation, "row must contain the unique string value")
	}
	body, err := json.Marshal(input.Values)
	if err != nil {
		return createRowFailure(CreateRowBranchDefect, sdkgo.FailureValidation, "row values are not JSON serializable")
	}
	fence, err := singledispatch.NewFence(call, CreateRowDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createRowFailure(CreateRowBranchDefect, sdkgo.FailureLocalDefect, "insert checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createRowFailure(CreateRowBranchDefect, sdkgo.FailureLocalDefect, "insert checkpoint is invalid")
	}
	if decision.Kind != singledispatch.SendAllowed {
		row, isFound, readFailure := operation.readCreatedRow(call, input)
		if readFailure != nil {
			if decision.Kind == singledispatch.ReadBackRequired {
				return createRowUncertain("insert could not be confirmed")
			}
			return sdkgo.NewMutationRetry[CreateRowOutput](*readFailure, 0)
		}
		if isFound {
			return sdkgo.NewMutationBranch(CreateRowBranchCreated, CreateRowOutput{Data: row}, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return createRowUncertain("insert outcome is unknown after the send limit")
		}
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.sendWithContext(call, requestContext, "createRow", http.MethodPost, "/"+input.Table, nil, body, false, http.StatusCreated)
		return nil
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[CreateRowOutput](*failure("createRow", sdkgo.FailureAvailability, "insert was not dispatched"), 0)
		}
		return createRowUncertain("insert outcome is unknown")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[CreateRowOutput](*failure("createRow", sdkgo.FailureAvailability, "retry checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[CreateRowOutput](*response.failure, response.retryAfter)
	}
	if response.retry {
		return sdkgo.NewMutationRetry[CreateRowOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		if response.branch == "providerRejected" {
			return sdkgo.NewMutationBranch(CreateRowBranchProviderRejected, CreateRowOutput{}, response.failure, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[CreateRowOutput](*failure("createRow", sdkgo.FailureProtocol, "insert response could not be confirmed"), 0)
	}
	var rows []json.RawMessage
	if json.Unmarshal(response.body, &rows) != nil || len(rows) != 1 {
		return sdkgo.NewMutationRetry[CreateRowOutput](*failure("createRow", sdkgo.FailureProtocol, "insert response did not contain one row"), 0)
	}
	return sdkgo.NewMutationBranch(CreateRowBranchCreated, CreateRowOutput{Data: rows[0]}, nil, sdkgo.Receipt{})
}

func (operation CreateRowOperation) readCreatedRow(call sdkgo.Call, input CreateRowInput) (json.RawMessage, bool, *sdkgo.Failure) {
	path, query, _ := rowFilter(input.Table, input.UniqueColumn, input.UniqueValue)
	query.Set("select", "*")
	query.Set("limit", "1")
	response := operation.client.send(call, "createRow", http.MethodGet, path, query, nil, false, http.StatusOK)
	if response.failure != nil {
		return nil, false, response.failure
	}
	var rows []json.RawMessage
	if json.Unmarshal(response.body, &rows) != nil || rows == nil || len(rows) > 1 {
		return nil, false, failure("createRow", sdkgo.FailureProtocol, "read-back response is invalid")
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	var actual map[string]any
	if json.Unmarshal(rows[0], &actual) != nil || actual == nil {
		return nil, false, failure("createRow", sdkgo.FailureProtocol, "read-back row is invalid")
	}
	expectedBody, _ := json.Marshal(input.Values)
	var expected map[string]any
	_ = json.Unmarshal(expectedBody, &expected)
	for column, value := range expected {
		if !reflect.DeepEqual(actual[column], value) {
			return nil, false, failure("createRow", sdkgo.FailureProtocol, "unique-key row does not match this insert")
		}
	}
	return rows[0], true, nil
}

func createRowFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreateRowOutput] {
	return sdkgo.NewMutationBranch(branch, CreateRowOutput{}, failure("createRow", kind, message), sdkgo.Receipt{})
}

func createRowUncertain(message string) sdkgo.MutationAttempt[CreateRowOutput] {
	return sdkgo.NewMutationUncertain(CreateRowOutput{}, *failure("createRow", sdkgo.FailureAvailability, message), sdkgo.Receipt{})
}
