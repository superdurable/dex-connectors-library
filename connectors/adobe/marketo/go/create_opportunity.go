// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"context"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateOpportunityOperation creates one opportunity with bounded sends.
type CreateOpportunityOperation struct{ client *Client }

type createOpportunityDispatch struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response leadResponse
}

// CreateOpportunity binds the Mutation to this client.
func (client *Client) CreateOpportunity() CreateOpportunityOperation {
	return CreateOpportunityOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreateOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return CreateOpportunityDefinition
}

// IdempotencyKey correlates bounded sends; Marketo does not receive the key.
func (CreateOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates by external key with bounded resends.
func (operation CreateOpportunityOperation) Invoke(call sdkgo.Call, input CreateOpportunityInput) sdkgo.MutationAttempt[OpportunityWriteResult] {
	if !validExternalOpportunityID(input.ExternalOpportunityID) || !validOpportunityName(input.Name) {
		return opportunityWriteBranch("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureValidation, "external opportunity ID or name is invalid")
	}
	body, err := encodeOpportunityBody("createOnly", input.ExternalOpportunityID, input.Name)
	if err != nil {
		return opportunityWriteBranch("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity create could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateOpportunityDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return opportunityWriteBranch("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity create checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return opportunityWriteRetry("createOpportunity", sdkgo.FailureAvailability, "opportunity create checkpoint is unavailable", 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return createOpportunityUncertain(call, "an earlier Marketo opportunity create may have succeeded and reached the send cap")
	}
	dispatch := &createOpportunityDispatch{client: operation.client, call: call, body: body}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return opportunityWriteRetry("createOpportunity", sdkgo.FailureAvailability, "opportunity create dispatch checkpoint is unavailable", 0)
	}
	response := dispatch.response
	if errors.Is(response.err, errLeadReauthorization) || errors.Is(response.err, errLeadCredentials) {
		if err := fence.RecordCanResend(); err != nil {
			return opportunityWriteRetry("createOpportunity", sdkgo.FailureAvailability, "opportunity create retry checkpoint is unavailable", 0)
		}
		if errors.Is(response.err, errLeadReauthorization) {
			return opportunityWriteBranch("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
		}
		return opportunityWriteRetry("createOpportunity", sdkgo.FailureTransport, "Marketo credentials are unavailable", 0)
	}
	if errors.Is(response.err, errLeadTransport) {
		return createOpportunityUnknown(call, decision, "Marketo opportunity create outcome is unknown")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return opportunityWriteRetry("createOpportunity", sdkgo.FailureAvailability, "opportunity create retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return opportunityWriteRetry("createOpportunity", kind, "Marketo temporarily rejected the opportunity create", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createOpportunityUnknown(call, decision, "Marketo opportunity create outcome is unknown")
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends == 0 && response.err == nil && isDefiniteMarketoRefusal(response.status, response.body) {
			if err := fence.RecordCanResend(); err != nil {
				return opportunityWriteRetry("createOpportunity", sdkgo.FailureAvailability, "opportunity refusal checkpoint is unavailable", 0)
			}
			return opportunityWriteBranch("createOpportunity", CreateOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the opportunity create")
		}
		return createOpportunityUnknown(call, decision, "Marketo opportunity create outcome is unknown")
	}
	if response.err != nil {
		return createOpportunityUnknown(call, decision, "Marketo opportunity create response could not be read completely")
	}
	result, outcome := decodeOpportunityResult(response.body)
	if kind, retry := classifyOpportunityCode(outcome); retry {
		return opportunityWriteRetry("createOpportunity", kind, "Marketo temporarily blocked the opportunity create", response.retryAfter)
	}
	if outcome != "" {
		if outcome == "1005" && decision.MaximumPriorSends == 0 {
			if err := fence.RecordCanResend(); err != nil {
				return opportunityWriteRetry("createOpportunity", sdkgo.FailureAvailability, "opportunity refusal checkpoint is unavailable", 0)
			}
			return opportunityWriteBranch("createOpportunity", CreateOpportunityBranchAlreadyExists, sdkgo.FailureProviderRejection, "Marketo already has a opportunity with this external ID")
		}
		return createOpportunityUnknown(call, decision, "Marketo opportunity create outcome cannot be confirmed")
	}
	if result.MarketoGUID == "" || result.Status != "created" {
		return createOpportunityUnknown(call, decision, "Marketo opportunity create returned an unexpected result")
	}
	_ = fence.RecordProviderID(result.MarketoGUID)
	value := OpportunityWriteResult{MarketoGUID: result.MarketoGUID, Status: "created"}
	return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, value, nil, opportunityReceipt(call, result.MarketoGUID))
}

func (dispatch *createOpportunityDispatch) execute(ctx context.Context) error {
	dispatch.response = dispatch.client.requestLeadAPIWithContext(ctx, dispatch.call, http.MethodPost, "/v1/opportunities.json", dispatch.body)
	return nil
}

func createOpportunityUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[OpportunityWriteResult] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "adobe-marketo", Operation: "createOpportunity", Message: message}
	return sdkgo.NewMutationUncertain(OpportunityWriteResult{}, failure, opportunityReceipt(call, ""))
}

func createOpportunityUnknown(call sdkgo.Call, decision singledispatch.Decision, message string) sdkgo.MutationAttempt[OpportunityWriteResult] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "adobe-marketo", Operation: "createOpportunity", Message: message}
	return limitedMarketoUnknown[OpportunityWriteResult](call, decision, failure, opportunityReceipt(call, ""))
}
