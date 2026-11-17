// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package insightly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateRecordInput supplies the provider fields for one new CRM record.
type CreateRecordInput struct {
	// RecordType selects the CRM resource.
	RecordType RecordType `json:"recordType"`
	// Fields are Insightly's uppercase JSON field names and exact JSON values.
	Fields map[string]json.RawMessage `json:"fields"`
}

// UpdateRecordInput changes the listed fields of one CRM record.
type UpdateRecordInput struct {
	// RecordType selects the CRM resource.
	RecordType RecordType `json:"recordType"`
	// ID is the positive numeric record identifier.
	ID int64 `json:"id"`
	// Fields are the new values, including null to clear a field.
	Fields map[string]json.RawMessage `json:"fields"`
}

// DeleteRecordInput identifies one CRM record to remove.
type DeleteRecordInput struct {
	// RecordType selects the CRM resource.
	RecordType RecordType `json:"recordType"`
	// ID is the positive numeric record identifier.
	ID int64 `json:"id"`
}

// DeletedRecord reports the removed record identity.
type DeletedRecord struct {
	// RecordType is the removed CRM resource.
	RecordType RecordType `json:"recordType"`
	// ID is the removed record identifier.
	ID int64 `json:"id"`
}

// CreateRecordOperation implements bounded-repeat record creation.
type CreateRecordOperation struct{ client *Client }

// UpdateRecordOperation implements a partial field update.
type UpdateRecordOperation struct{ client *Client }

// DeleteRecordOperation implements repeat-safe record removal.
type DeleteRecordOperation struct{ client *Client }

// CreateRecord binds CRM record creation to this client.
func (client *Client) CreateRecord() CreateRecordOperation {
	return CreateRecordOperation{client: client}
}

// UpdateRecord binds CRM field updates to this client.
func (client *Client) UpdateRecord() UpdateRecordOperation {
	return UpdateRecordOperation{client: client}
}

// DeleteRecord binds CRM record deletion to this client.
func (client *Client) DeleteRecord() DeleteRecordOperation {
	return DeleteRecordOperation{client: client}
}

// Definition returns the generated create Mutation contract.
func (CreateRecordOperation) Definition() sdkgo.MutationDefinition { return CreateRecordDefinition }

// Definition returns the generated update Mutation contract.
func (UpdateRecordOperation) Definition() sdkgo.MutationDefinition { return UpdateRecordDefinition }

// Definition returns the generated delete Mutation contract.
func (DeleteRecordOperation) Definition() sdkgo.MutationDefinition { return DeleteRecordDefinition }

// IdempotencyKey correlates attempts; Insightly does not deduplicate creates by this value.
func (CreateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies repeated partial updates of one Step.
func (UpdateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies repeated deletions of one Step.
func (DeleteRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

type insightlyExchange struct {
	status     int
	body       []byte
	retryAfter time.Duration
	err        error
}

func (client *Client) exchange(ctx context.Context, call sdkgo.Call, method, path string, body []byte) insightlyExchange {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return insightlyExchange{err: errCredential}
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, reader)
	if err != nil {
		return insightlyExchange{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.SetBasicAuth(credentials.APIKey.Reveal(), "")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return insightlyExchange{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, readError := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return insightlyExchange{status: response.StatusCode, body: responseBody, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: readError}
}

// Invoke creates one record, reconciling a returned ID before any later send.
func (operation CreateRecordOperation) Invoke(call sdkgo.Call, input CreateRecordInput) sdkgo.MutationAttempt[Record] {
	const operationID = "createRecord"
	if err := input.RecordType.validate(); err != nil {
		return recordMutationFailure[Record](operationID, CreateRecordBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if err := validateRecordFields(input.Fields, input.RecordType.idField()); err != nil {
		return recordMutationFailure[Record](operationID, CreateRecordBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	body, err := json.Marshal(input.Fields)
	if err != nil {
		return recordMutationFailure[Record](operationID, CreateRecordBranchDefect, sdkgo.FailureLocalDefect, "create body cannot be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateRecordDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return recordMutationFailure[Record](operationID, CreateRecordBranchDefect, sdkgo.FailureLocalDefect, "create checkpoint cannot be built")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return recordMutationUncertain[Record](operationID, sdkgo.FailureTransport, "create checkpoint cannot be trusted")
	}
	if decision.Marker.ProviderID != "" {
		readBack := operation.client.exchange(call.Context, call, http.MethodGet, input.RecordType.path()+"/"+decision.Marker.ProviderID, nil)
		if readBack.status == http.StatusOK && readBack.err == nil {
			record, decodeError := decodeRecord(input.RecordType, readBack.body)
			if decodeError == nil && strconv.FormatInt(record.ID, 10) == decision.Marker.ProviderID {
				return sdkgo.NewMutationBranch(CreateRecordBranchCreated, record, nil, sdkgo.Receipt{})
			}
		}
		return recordMutationUncertain[Record](operationID, sdkgo.FailureTransport, "created record cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return recordMutationUncertain[Record](operationID, sdkgo.FailureTransport, "prior create cannot be confirmed")
	}
	var result insightlyExchange
	var didGetConnection atomic.Bool
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		result = operation.client.exchange(httptrace.WithClientTrace(requestContext, trace), call, http.MethodPost, input.RecordType.path(), body)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return recordMutationRetry[Record](operationID, sdkgo.FailureAvailability, "create dispatch did not start", 0)
	}
	if err != nil {
		return recordMutationUncertain[Record](operationID, sdkgo.FailureTransport, "create dispatch cannot be confirmed")
	}
	if result.status == http.StatusTooManyRequests || result.status == http.StatusServiceUnavailable || (errors.Is(result.err, errRequest) && !didGetConnection.Load()) {
		if checkpointError := fence.RecordCanResend(); checkpointError != nil {
			return recordMutationRetry[Record](operationID, sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if result.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		if errors.Is(result.err, errRequest) {
			kind = sdkgo.FailureTransport
		}
		return recordMutationRetry[Record](operationID, kind, "Insightly deferred or did not receive the create", result.retryAfter)
	}
	if result.status == http.StatusRequestTimeout || result.status >= 500 || errors.Is(result.err, errRequest) {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return recordMutationRetry[Record](operationID, sdkgo.FailureAvailability, "create outcome is unknown", result.retryAfter)
		}
		return recordMutationUncertain[Record](operationID, sdkgo.FailureAvailability, "create outcome is unknown")
	}
	if errors.Is(result.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return recordMutationUncertain[Record](operationID, sdkgo.FailureAuthentication, "prior create remains unconfirmed")
		}
		return recordMutationFailure[Record](operationID, CreateRecordBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if result.status < 200 || result.status >= 300 {
		if decision.MaximumPriorSends > 0 {
			return recordMutationUncertain[Record](operationID, sdkgo.FailureTransport, "prior create remains unconfirmed")
		}
		return recordMutationFailure[Record](operationID, CreateRecordBranchProviderRejected, sdkgo.FailureProviderRejection, "Insightly rejected HTTP "+strconv.Itoa(result.status))
	}
	if result.err != nil || result.status != http.StatusCreated {
		return recordMutationUncertain[Record](operationID, sdkgo.FailureProtocol, "Insightly create response cannot be confirmed")
	}
	record, err := decodeRecord(input.RecordType, result.body)
	if err != nil {
		return recordMutationUncertain[Record](operationID, sdkgo.FailureProtocol, "Insightly returned an invalid created record")
	}
	// The accepted result remains known if its read-back checkpoint fails.
	_ = fence.RecordProviderID(strconv.FormatInt(record.ID, 10))
	return sdkgo.NewMutationBranch(CreateRecordBranchCreated, record, nil, sdkgo.Receipt{})
}

// Invoke changes only the listed fields, repeating the same values safely.
func (operation UpdateRecordOperation) Invoke(call sdkgo.Call, input UpdateRecordInput) sdkgo.MutationAttempt[Record] {
	const operationID = "updateRecord"
	if err := input.RecordType.validate(); err != nil {
		return recordMutationFailure[Record](operationID, UpdateRecordBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.ID <= 0 {
		return recordMutationFailure[Record](operationID, UpdateRecordBranchDefect, sdkgo.FailureValidation, "record ID must be positive")
	}
	if err := validateRecordFields(input.Fields, input.RecordType.idField()); err != nil {
		return recordMutationFailure[Record](operationID, UpdateRecordBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	fields := make(map[string]json.RawMessage, len(input.Fields)+1)
	for name, value := range input.Fields {
		fields[name] = value
	}
	fields[input.RecordType.idField()] = json.RawMessage(strconv.FormatInt(input.ID, 10))
	body, err := json.Marshal(fields)
	if err != nil {
		return recordMutationFailure[Record](operationID, UpdateRecordBranchDefect, sdkgo.FailureLocalDefect, "update body cannot be encoded")
	}
	result := operation.client.exchange(call.Context, call, http.MethodPut, input.RecordType.path(), body)
	if attempt, handled := classifySafeRecordWrite[Record](operationID, result, UpdateRecordBranchNotFound, UpdateRecordBranchProviderRejected, UpdateRecordBranchDefect); handled {
		return attempt
	}
	if result.err != nil {
		return recordMutationRetry[Record](operationID, sdkgo.FailureTransport, "update response was interrupted", 0)
	}
	record, err := decodeRecord(input.RecordType, result.body)
	if err != nil || record.ID != input.ID {
		return recordMutationFailure[Record](operationID, UpdateRecordBranchInvalidResponse, sdkgo.FailureProtocol, "Insightly returned an invalid updated record")
	}
	return sdkgo.NewMutationBranch(UpdateRecordBranchUpdated, record, nil, sdkgo.Receipt{})
}

// Invoke removes a record; an already absent ID satisfies the requested end state.
func (operation DeleteRecordOperation) Invoke(call sdkgo.Call, input DeleteRecordInput) sdkgo.MutationAttempt[DeletedRecord] {
	const operationID = "deleteRecord"
	if err := input.RecordType.validate(); err != nil {
		return recordMutationFailure[DeletedRecord](operationID, DeleteRecordBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.ID <= 0 {
		return recordMutationFailure[DeletedRecord](operationID, DeleteRecordBranchDefect, sdkgo.FailureValidation, "record ID must be positive")
	}
	result := operation.client.exchange(call.Context, call, http.MethodDelete, input.RecordType.path()+"/"+strconv.FormatInt(input.ID, 10), nil)
	if result.status == http.StatusNotFound || (result.status >= 200 && result.status < 300 && result.err == nil) {
		return sdkgo.NewMutationBranch(DeleteRecordBranchDeleted, DeletedRecord{RecordType: input.RecordType, ID: input.ID}, nil, sdkgo.Receipt{})
	}
	if attempt, handled := classifySafeRecordWrite[DeletedRecord](operationID, result, "", DeleteRecordBranchProviderRejected, DeleteRecordBranchDefect); handled {
		return attempt
	}
	return recordMutationRetry[DeletedRecord](operationID, sdkgo.FailureTransport, "delete response was interrupted", 0)
}

func validateRecordFields(fields map[string]json.RawMessage, idField string) error {
	if len(fields) == 0 || len(fields) > 100 {
		return errors.New("record requires 1 through 100 fields")
	}
	for name, value := range fields {
		if !insightlyFieldNamePattern.MatchString(name) || name == idField || !json.Valid(value) {
			return errors.New("record field name or JSON value is invalid")
		}
	}
	return nil
}

func classifySafeRecordWrite[T any](operation string, result insightlyExchange, notFound, rejected, defect sdkgo.BranchID) (sdkgo.MutationAttempt[T], bool) {
	var zero sdkgo.MutationAttempt[T]
	if errors.Is(result.err, errCredential) {
		return recordMutationFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials are unavailable"), true
	}
	if errors.Is(result.err, errRequest) {
		return recordMutationRetry[T](operation, sdkgo.FailureTransport, "Insightly request failed", 0), true
	}
	if result.status == http.StatusTooManyRequests {
		return recordMutationRetry[T](operation, sdkgo.FailureRateLimit, "Insightly rate limited the request", result.retryAfter), true
	}
	if result.status == http.StatusRequestTimeout || result.status >= 500 {
		return recordMutationRetry[T](operation, sdkgo.FailureAvailability, "Insightly is temporarily unavailable", result.retryAfter), true
	}
	if result.status == http.StatusNotFound && notFound != "" {
		return recordMutationFailure[T](operation, notFound, sdkgo.FailureNotFound, "Insightly did not find the record"), true
	}
	if result.status < 200 || result.status >= 300 {
		return recordMutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "Insightly rejected HTTP "+strconv.Itoa(result.status)), true
	}
	return zero, false
}

func recordMutationFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func recordMutationRetry[T any](operation string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](queryFailure(operation, kind, message), after)
}

func recordMutationUncertain[T any](operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	return sdkgo.NewMutationUncertain(zero, queryFailure(operation, kind, message), sdkgo.Receipt{})
}
