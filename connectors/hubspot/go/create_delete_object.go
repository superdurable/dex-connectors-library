// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Definition returns the immutable create operation definition.
func (CreateObjectOperation) Definition() sdkgo.MutationDefinition { return CreateObjectDefinition }

// IdempotencyKey correlates attempts; HubSpot does not deduplicate creates by this value.
func (CreateObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a record with bounded resends and read-back when HubSpot returned its ID.
func (operation CreateObjectOperation) Invoke(call sdkgo.Call, input CreateObjectInput) sdkgo.MutationAttempt[CRMObject] {
	const operationID = "createObject"
	if err := input.ObjectType.validate(); err != nil {
		return createObjectFailure(CreateObjectBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if err := validatePropertyValues(input.Properties, true); err != nil {
		return createObjectFailure(CreateObjectBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	body, err := json.Marshal(hubspotUpdateRequest{Properties: input.Properties})
	if err != nil {
		return createObjectFailure(CreateObjectBranchDefect, sdkgo.FailureLocalDefect, "create request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateObjectDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createObjectFailure(CreateObjectBranchDefect, sdkgo.FailureLocalDefect, "create checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport, "create checkpoint cannot be trusted"), sdkgo.Receipt{})
	}
	if decision.Marker.ProviderID != "" {
		readResponse, readOutcome := operation.client.exchange(call, operationID, providerRequest{method: http.MethodGet, path: objectsPath(input.ObjectType, decision.Marker.ProviderID)}, decision.Marker.ProviderID)
		if readOutcome == nil {
			object, decodeError := decodeObject(input.ObjectType, readResponse.body)
			if decodeError == nil && object.ID == decision.Marker.ProviderID {
				return sdkgo.NewMutationBranch(CreateObjectBranchCreated, object, nil, operation.client.receipt(call, readResponse, object.ID))
			}
		}
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport, "created record could not be confirmed"), sdkgo.Receipt{})
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport, "prior create cannot be confirmed"), sdkgo.Receipt{})
	}
	request := providerRequest{method: http.MethodPost, path: objectsCollectionPath(input.ObjectType), body: body}
	var response providerResponse
	var outcome *providerOutcome
	var didGetConnection atomic.Bool
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		response, outcome = operation.client.exchangeWithContext(httptrace.WithClientTrace(requestContext, trace), call, operationID, request, "")
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CRMObject](providerFailure(operationID, sdkgo.FailureAvailability, "create dispatch did not start"), 0)
	}
	if err != nil {
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport, "create dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	if outcome != nil {
		if outcome.disposition == outcomeRetry && (response.statusCode == http.StatusTooManyRequests || response.statusCode == http.StatusServiceUnavailable || response.statusCode == http.StatusLocked || !didGetConnection.Load()) {
			if checkpointError := fence.RecordCanResend(); checkpointError != nil {
				return sdkgo.NewMutationRetry[CRMObject](providerFailure(operationID, sdkgo.FailureAvailability, "retry checkpoint is unavailable"), 0)
			}
			return sdkgo.NewMutationRetry[CRMObject](outcome.failure, outcome.retryAfter)
		}
		if outcome.disposition == outcomeRetry {
			if decision.MaximumPriorSends+1 < call.MaxSends() {
				return sdkgo.NewMutationRetry[CRMObject](outcome.failure, outcome.retryAfter)
			}
			return sdkgo.NewMutationUncertain(CRMObject{}, outcome.failure, outcome.receipt)
		}
		if response.statusCode >= 200 && response.statusCode < 300 {
			return sdkgo.NewMutationUncertain(CRMObject{}, outcome.failure, outcome.receipt)
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport, "prior create remains unconfirmed"), outcome.receipt)
		}
		return mutationAttemptForOutcome[CRMObject](*outcome, operationBranches{conflict: CreateObjectBranchConflict, rejected: CreateObjectBranchProviderRejected, defect: CreateObjectBranchDefect})
	}
	receipt := operation.client.receipt(call, response, "")
	if response.statusCode != http.StatusCreated {
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureProtocol, "HubSpot returned an unexpected create status"), receipt)
	}
	object, err := decodeObject(input.ObjectType, response.body)
	if err != nil {
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureProtocol, "HubSpot returned an invalid created record"), receipt)
	}
	receipt.ProviderObjectID = object.ID
	// The accepted result remains known if its read-back checkpoint fails.
	_ = fence.RecordProviderID(object.ID)
	return sdkgo.NewMutationBranch(CreateObjectBranchCreated, object, nil, receipt)
}

func createObjectFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CRMObject] {
	return sdkgo.NewMutationBranch(branch, CRMObject{}, providerFailurePointer("createObject", kind, message), sdkgo.Receipt{})
}

// Definition returns the immutable delete operation definition.
func (DeleteObjectOperation) Definition() sdkgo.MutationDefinition { return DeleteObjectDefinition }

// IdempotencyKey identifies repeated archive attempts of one Step.
func (DeleteObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke archives a record; an already absent record satisfies the requested end state.
func (operation DeleteObjectOperation) Invoke(call sdkgo.Call, input DeleteObjectInput) sdkgo.MutationAttempt[DeletedObject] {
	const operationID = "deleteObject"
	if err := input.ObjectType.validate(); err != nil {
		return sdkgo.NewMutationBranch(DeleteObjectBranchDefect, DeletedObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	if !recordIDPattern.MatchString(input.ObjectID) {
		return sdkgo.NewMutationBranch(DeleteObjectBranchDefect, DeletedObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, "object ID must be a decimal HubSpot record ID"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, operationID, providerRequest{method: http.MethodDelete, path: objectsPath(input.ObjectType, input.ObjectID)}, input.ObjectID)
	if outcome != nil && outcome.disposition != outcomeNotFound {
		return mutationAttemptForOutcome[DeletedObject](*outcome, operationBranches{rejected: DeleteObjectBranchProviderRejected, defect: DeleteObjectBranchDefect})
	}
	return sdkgo.NewMutationBranch(DeleteObjectBranchDeleted, DeletedObject{ObjectType: input.ObjectType, ObjectID: input.ObjectID}, nil, operation.client.receipt(call, response, input.ObjectID))
}
