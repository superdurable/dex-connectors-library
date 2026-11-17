// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateObjectInput is one new record. Fields use Pipedrive API v2 field
// names, such as title, person_id, org_id, stage_id, owner_id, and value for a
// deal, or name, org_id, and emails for a person; each value is exact JSON.
type CreateObjectInput struct {
	// ObjectType is the type of record to create.
	ObjectType ObjectType `json:"objectType"`
	// Fields maps standard field names to JSON values. A deal requires title;
	// a person or organization requires name.
	Fields map[string]json.RawMessage `json:"fields"`
	// CustomFields maps custom field keys, the 40-character keys shown in
	// Company settings > Data fields, to JSON values.
	CustomFields map[string]json.RawMessage `json:"customFields,omitempty"`
}

// CreateObjectOperation implements the single-dispatch create Mutation.
type CreateObjectOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (CreateObjectOperation) Definition() sdkgo.MutationDefinition { return CreateObjectDefinition }

// IdempotencyKey uses the stable connector Call ID. Pipedrive documents no
// idempotency key, so the key only correlates the Receipt; a Dex heartbeat
// checkpoint narrows duplicate sends instead.
func (CreateObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a record, resending an unknown outcome only while possible sends stay below the send cap.
func (operation CreateObjectOperation) Invoke(call sdkgo.Call, input CreateObjectInput) sdkgo.MutationAttempt[CRMObject] {
	const operationID = "createObject"
	fence, err := singledispatch.NewFence(call, CreateObjectDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureLocalDefect, "single-dispatch fence could not be built"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return singleDispatchRetry[CRMObject](operationID)
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		// Pipedrive returns an ID only for a record it created, so a saved ID is read back, never resent.
		if decision.Marker.ProviderID != "" && decision.Marker.CallID == call.ID {
			return operation.readBackCreatedRecord(call, input.ObjectType, decision.Marker.ProviderID)
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport,
				"earlier creates may have reached Pipedrive up to the send cap and cannot be confirmed"), operation.client.receipt(call, providerResponse{}, ""))
		}
	}
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	body, err := input.createRequestBody()
	if err != nil {
		return sdkgo.NewMutationBranch(CreateObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, operationID)
	if failure != nil {
		// A pre-send Retry after an earlier possible send records nothing, so the next attempt still counts it.
		if decision.Kind == singledispatch.SendAllowed {
			if err := fence.RecordNotSent(); err != nil {
				return singleDispatchRetry[CRMObject](operationID)
			}
		}
		if hasPossibleEarlierSend && failure.route != sessionRetry {
			return sdkgo.NewMutationUncertain(CRMObject{}, failure.failure, operation.client.receipt(call, providerResponse{}, ""))
		}
		return mutationAttemptForSession[CRMObject](failure, operationBranches{rejected: CreateObjectBranchProviderRejected, defect: CreateObjectBranchDefect})
	}
	defer cancel()
	send := &createObjectSend{client: operation.client, session: session, request: providerRequest{method: http.MethodPost, path: collectionPath(input.ObjectType), body: body, isSingleDispatch: true}}
	err = fence.Dispatch(send.execute)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return singleDispatchRetry[CRMObject](operationID)
	}
	if err != nil {
		return resendOrSelectUncertainCreate(canResendUnknownOutcome, providerFailure(operationID, sdkgo.FailureTransport, "create dispatch could not be confirmed"), sdkgo.Receipt{}, 0)
	}
	result := send.result
	receipt := operation.client.receipt(call, result.response, "")
	if result.response.statusCode == http.StatusUnauthorized && session.credentials.AuthMethodID == OAuthAuthMethodID {
		refreshed, refreshError := operation.client.refreshCredentialAfterRejection(session)
		if refreshed {
			if err := fence.RecordCanResend(); err != nil {
				return singleDispatchRetry[CRMObject](operationID)
			}
			return sdkgo.NewMutationRetry[CRMObject](result.failure, 0)
		}
		result.failure = rejectedWriteFailure(operationID, refreshError)
	}
	isAccepted := result.response.statusCode >= 200 && result.response.statusCode < 300
	switch {
	case result.outcome == exchangeSucceeded:
	case result.outcome == exchangeNotSent || result.outcome == exchangeRateLimited ||
		(result.outcome == exchangeUnavailable && result.response.statusCode == http.StatusServiceUnavailable):
		if err := fence.RecordCanResend(); err != nil {
			return singleDispatchRetry[CRMObject](operationID)
		}
		return sdkgo.NewMutationRetry[CRMObject](result.failure, result.retryAfter)
	case result.outcome == exchangeInvalid || isAccepted:
		// Pipedrive accepted this POST, so a resend would duplicate the record.
		return sdkgo.NewMutationUncertain(CRMObject{}, result.failure, receipt)
	case result.outcome == exchangeDefect && !hasPossibleEarlierSend:
		return sdkgo.NewMutationBranch(CreateObjectBranchDefect, CRMObject{}, &result.failure, receipt)
	case isDefiniteWriteRefusal(result) && (result.outcome == exchangeRejected || result.outcome == exchangeNotFound):
		// An earlier send may have created the record, so a refusal of this resend proves nothing.
		if hasPossibleEarlierSend {
			return sdkgo.NewMutationUncertain(CRMObject{}, result.failure, receipt)
		}
		if err := fence.RecordCanResend(); err != nil {
			return singleDispatchRetry[CRMObject](operationID)
		}
		return sdkgo.NewMutationBranch(CreateObjectBranchProviderRejected, CRMObject{}, &result.failure, receipt)
	default:
		// A timeout, 5xx, 408, 409, 402, redirect, or other status leaves the outcome unknown.
		return resendOrSelectUncertainCreate(canResendUnknownOutcome, result.failure, receipt, result.retryAfter)
	}
	object, err := decodeRecordBody(input.ObjectType, result.response.body)
	if err != nil {
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureProtocol,
			"Pipedrive accepted the create but returned an invalid record"), receipt)
	}
	receipt.ProviderObjectID = object.ID
	// The accepted result remains known if its extra read-back checkpoint is lost.
	_ = fence.RecordProviderID(object.ID)
	return sdkgo.NewMutationBranch(CreateObjectBranchCreated, object, nil, receipt)
}

// readBackCreatedRecord confirms a create whose provider ID an earlier attempt saved.
func (operation CreateObjectOperation) readBackCreatedRecord(call sdkgo.Call, objectType ObjectType, objectID string) sdkgo.MutationAttempt[CRMObject] {
	session, cancel, failure := operation.client.startSession(call, "createObject")
	if failure != nil {
		if failure.route == sessionRetry {
			return sdkgo.NewMutationRetry[CRMObject](failure.failure, 0)
		}
		return sdkgo.NewMutationUncertain(CRMObject{}, failure.failure, operation.client.receipt(call, providerResponse{}, objectID))
	}
	defer cancel()
	object, result := operation.client.readRecord(session, objectType, objectID)
	if result.outcome == exchangeSucceeded {
		return sdkgo.NewMutationBranch(CreateObjectBranchCreated, object, nil, operation.client.receipt(call, result.response, object.ID))
	}
	if isRetryableExchange(result.outcome) {
		return sdkgo.NewMutationRetry[CRMObject](result.failure, result.retryAfter)
	}
	return sdkgo.NewMutationUncertain(CRMObject{}, result.failure, operation.client.receipt(call, result.response, objectID))
}

// resendOrSelectUncertainCreate retries a possibly sent POST while the send cap allows another send.
func resendOrSelectUncertainCreate(canResendUnknownOutcome bool, failure sdkgo.Failure, receipt sdkgo.Receipt, retryAfter time.Duration) sdkgo.MutationAttempt[CRMObject] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[CRMObject](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(CRMObject{}, failure, receipt)
}

type createObjectSend struct {
	client  *Client
	session *operationSession
	request providerRequest
	result  pipedriveExchange
}

func (send *createObjectSend) execute(ctx context.Context) error {
	send.request.context = ctx
	send.result = send.client.exchangeOnce(send.session, send.request)
	return nil
}

func (input CreateObjectInput) createRequestBody() ([]byte, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	if err := validateWrittenFields(input.Fields, input.CustomFields); err != nil {
		return nil, err
	}
	if err := requireNameField(input.ObjectType, input.Fields); err != nil {
		return nil, err
	}
	return encodeWriteBody(input.Fields, input.CustomFields)
}
