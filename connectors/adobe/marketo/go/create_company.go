// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateCompanyOperation creates one company with bounded sends.
type CreateCompanyOperation struct{ client *Client }

type createCompanyDispatch struct {
	client   *Client
	call     sdkgo.Call
	body     []byte
	response leadResponse
}

// CreateCompany binds the Mutation to this client.
func (client *Client) CreateCompany() CreateCompanyOperation { return CreateCompanyOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateCompanyOperation) Definition() sdkgo.MutationDefinition { return CreateCompanyDefinition }

// IdempotencyKey correlates bounded sends; Marketo does not receive the key.
func (CreateCompanyOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCompanyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates by external key with bounded resends.
func (operation CreateCompanyOperation) Invoke(call sdkgo.Call, input CreateCompanyInput) sdkgo.MutationAttempt[CompanyWriteResult] {
	if !validExternalCompanyID(input.ExternalCompanyID) || !validCompanyName(input.Company) {
		return companyWriteBranch("createCompany", CreateCompanyBranchDefect, sdkgo.FailureValidation, "external company ID or name is invalid")
	}
	body, err := encodeCompanyBody("createOnly", input.ExternalCompanyID, input.Company)
	if err != nil {
		return companyWriteBranch("createCompany", CreateCompanyBranchDefect, sdkgo.FailureLocalDefect, "company create could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateCompanyDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return companyWriteBranch("createCompany", CreateCompanyBranchDefect, sdkgo.FailureLocalDefect, "company create checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "company create checkpoint is unavailable", 0)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return createCompanyUncertain(call, "an earlier Marketo company create may have succeeded and reached the send cap")
	}
	dispatch := &createCompanyDispatch{client: operation.client, call: call, body: body}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "company create dispatch checkpoint is unavailable", 0)
	}
	response := dispatch.response
	if errors.Is(response.err, errLeadReauthorization) || errors.Is(response.err, errLeadCredentials) {
		if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
			return createCompanyUnknown(call, decision, "company create remains unconfirmed after credentials became unavailable")
		}
		if err := fence.RecordCanResend(); err != nil {
			return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "company create retry checkpoint is unavailable", 0)
		}
		if errors.Is(response.err, errLeadReauthorization) {
			return companyWriteBranch("createCompany", CreateCompanyBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
		}
		return companyWriteRetry("createCompany", sdkgo.FailureTransport, "Marketo credentials are unavailable", 0)
	}
	if errors.Is(response.err, errLeadTransport) {
		return createCompanyUnknown(call, decision, "Marketo company create outcome is unknown")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "company create retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return companyWriteRetry("createCompany", kind, "Marketo temporarily rejected the company create", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createCompanyUnknown(call, decision, "Marketo company create outcome is unknown")
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends == 0 && response.err == nil && isDefiniteMarketoRefusal(response.status, response.body) {
			if err := fence.RecordCanResend(); err != nil {
				return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "company refusal checkpoint is unavailable", 0)
			}
			return companyWriteBranch("createCompany", CreateCompanyBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company create")
		}
		return createCompanyUnknown(call, decision, "Marketo company create outcome is unknown")
	}
	if response.err != nil {
		return createCompanyUnknown(call, decision, "Marketo company create response could not be read completely")
	}
	result, outcome := decodeCompanyResult(response.body)
	if kind, retry := classifyCompanyCode(outcome); retry {
		return companyWriteRetry("createCompany", kind, "Marketo temporarily blocked the company create", response.retryAfter)
	}
	if outcome != "" {
		if outcome == "1005" && decision.MaximumPriorSends == 0 {
			if err := fence.RecordCanResend(); err != nil {
				return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "company refusal checkpoint is unavailable", 0)
			}
			return companyWriteBranch("createCompany", CreateCompanyBranchAlreadyExists, sdkgo.FailureProviderRejection, "Marketo already has a company with this external ID")
		}
		return createCompanyUnknown(call, decision, "Marketo company create outcome cannot be confirmed")
	}
	if result.ID <= 0 || result.Status != "created" {
		return createCompanyUnknown(call, decision, "Marketo company create returned an unexpected result")
	}
	_ = fence.RecordProviderID(strconv.FormatInt(result.ID, 10))
	value := CompanyWriteResult{ID: result.ID, Status: "created"}
	return sdkgo.NewMutationBranch(CreateCompanyBranchCreated, value, nil, companyReceipt(call, result.ID))
}

func (dispatch *createCompanyDispatch) execute(ctx context.Context) error {
	dispatch.response = dispatch.client.requestLeadAPIWithContext(ctx, dispatch.call, http.MethodPost, "/v1/companies.json", dispatch.body)
	return nil
}

func createCompanyUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[CompanyWriteResult] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "adobe-marketo", Operation: "createCompany", Message: message}
	return sdkgo.NewMutationUncertain(CompanyWriteResult{}, failure, companyReceipt(call, 0))
}

func createCompanyUnknown(call sdkgo.Call, decision singledispatch.Decision, message string) sdkgo.MutationAttempt[CompanyWriteResult] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "adobe-marketo", Operation: "createCompany", Message: message}
	return limitedMarketoUnknown[CompanyWriteResult](call, decision, failure, companyReceipt(call, 0))
}
