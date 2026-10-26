// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

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

// Invoke creates a record once, retrying only when the provider did not process the request.
func (operation CreateObjectOperation) Invoke(call sdkgo.Call, input CreateObjectInput) sdkgo.MutationAttempt[CRMObject] {
	const operationID = "createObject"
	fence, err := singledispatch.NewFence(call, CreateObjectDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureLocalDefect, "single-dispatch fence could not be built"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind != singledispatch.SendAllowed {
		if decision.Kind == singledispatch.ReadBackRequired && decision.Marker.ProviderID != "" && decision.Marker.CallID == call.ID {
			session, cancel, failure := operation.client.startSession(call, operationID)
			if failure != nil {
				if failure.route == sessionRetry {
					return sdkgo.NewMutationRetry[CRMObject](failure.failure, 0)
				}
				return sdkgo.NewMutationUncertain(CRMObject{}, failure.failure, sdkgo.Receipt{})
			}
			defer cancel()
			object, result := operation.client.readRecord(session, input.ObjectType, decision.Marker.ProviderID)
			if result.outcome == exchangeSucceeded {
				return sdkgo.NewMutationBranch(CreateObjectBranchCreated, object, nil, operation.client.receipt(call, result.response, object.ID))
			}
			if isRetryableExchange(result.outcome) {
				return sdkgo.NewMutationRetry[CRMObject](result.failure, result.retryAfter)
			}
			return sdkgo.NewMutationUncertain(CRMObject{}, result.failure, operation.client.receipt(call, result.response, decision.Marker.ProviderID))
		}
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport,
			"an earlier create may have reached Pipedrive and cannot be confirmed"), operation.client.receipt(call, providerResponse{}, ""))
	}
	body, err := input.createRequestBody()
	if err != nil {
		return sdkgo.NewMutationBranch(CreateObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, operationID)
	if failure != nil {
		if err := fence.RecordNotSent(); err != nil {
			return singleDispatchRetry[CRMObject](operationID)
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
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport, "create dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	result := send.result
	receipt := operation.client.receipt(call, result.response, "")
	if result.response.statusCode == http.StatusUnauthorized && session.credentials.AuthMethodID == OAuthAuthMethodID && operation.client.refreshCredentialAfterRejection(session) {
		if err := fence.RecordCanResend(); err != nil {
			return singleDispatchRetry[CRMObject](operationID)
		}
		return sdkgo.NewMutationRetry[CRMObject](result.failure, 0)
	}
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeNotSent, exchangeRateLimited:
		if err := fence.RecordCanResend(); err != nil {
			return singleDispatchRetry[CRMObject](operationID)
		}
		return sdkgo.NewMutationRetry[CRMObject](result.failure, result.retryAfter)
	case exchangeUnavailable:
		if result.response.statusCode == http.StatusServiceUnavailable {
			if err := fence.RecordCanResend(); err != nil {
				return singleDispatchRetry[CRMObject](operationID)
			}
			return sdkgo.NewMutationRetry[CRMObject](result.failure, result.retryAfter)
		}
		return sdkgo.NewMutationUncertain(CRMObject{}, result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewMutationUncertain(CRMObject{}, result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(CreateObjectBranchDefect, CRMObject{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(CreateObjectBranchProviderRejected, CRMObject{}, &result.failure, receipt)
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
