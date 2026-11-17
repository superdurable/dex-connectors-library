// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
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

// Invoke creates by external key and reads back before a bounded resend.
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
		return companyWriteBranch("createCompany", CreateCompanyBranchDefect, sdkgo.FailureLocalDefect, "company create checkpoint is invalid")
	}
	if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
		found, readStatus := operation.readBackCreatedCompany(call, input)
		switch readStatus {
		case "found":
			return sdkgo.NewMutationBranch(CreateCompanyBranchCreated, found, nil, companyReceipt(call, found.ID))
		case "retry":
			return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "Marketo company read-back is temporarily unavailable", 0)
		case "uncertain":
			return createCompanyUncertain(call, "Marketo company read-back cannot confirm the earlier create")
		}
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
		if err := fence.RecordCanResend(); err != nil {
			return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "company create retry checkpoint is unavailable", 0)
		}
		if errors.Is(response.err, errLeadReauthorization) {
			return companyWriteBranch("createCompany", CreateCompanyBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
		}
		return companyWriteRetry("createCompany", sdkgo.FailureTransport, "Marketo credentials are unavailable", 0)
	}
	if errors.Is(response.err, errLeadTransport) {
		return companyWriteRetry("createCompany", sdkgo.FailureTransport, "Marketo company create outcome is unknown", 0)
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
		return companyWriteRetry("createCompany", sdkgo.FailureAvailability, "Marketo company create outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return createCompanyUncertain(call, "an earlier Marketo company create may have succeeded")
		}
		return companyWriteBranch("createCompany", CreateCompanyBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company create")
	}
	if response.err != nil {
		return createCompanyUncertain(call, "Marketo company create response could not be read completely")
	}
	result, outcome := decodeCompanyResult(response.body)
	if kind, retry := classifyCompanyCode(outcome); retry {
		return companyWriteRetry("createCompany", kind, "Marketo temporarily blocked the company create", response.retryAfter)
	}
	if outcome != "" {
		if outcome == "1005" && decision.MaximumPriorSends == 0 {
			return companyWriteBranch("createCompany", CreateCompanyBranchAlreadyExists, sdkgo.FailureProviderRejection, "Marketo already has a company with this external ID")
		}
		if decision.MaximumPriorSends > 0 || outcome == "invalid" {
			return createCompanyUncertain(call, "Marketo company create outcome cannot be confirmed")
		}
		return companyWriteBranch("createCompany", CreateCompanyBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the company create")
	}
	if result.ID <= 0 || result.Status != "created" {
		return createCompanyUncertain(call, "Marketo accepted the company create but returned an unexpected result")
	}
	_ = fence.RecordProviderID(strconv.FormatInt(result.ID, 10))
	value := CompanyWriteResult{ID: result.ID, Status: "created"}
	return sdkgo.NewMutationBranch(CreateCompanyBranchCreated, value, nil, companyReceipt(call, result.ID))
}

func (operation CreateCompanyOperation) readBackCreatedCompany(call sdkgo.Call, input CreateCompanyInput) (CompanyWriteResult, string) {
	query := url.Values{"filterType": {"externalCompanyId"}, "filterValues": {input.ExternalCompanyID}, "fields": {"id,company"}}
	response := operation.client.requestLeadAPI(call, http.MethodGet, "/v1/companies.json?"+query.Encode(), nil)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return CompanyWriteResult{}, "retry"
	}
	if response.status != http.StatusOK {
		return CompanyWriteResult{}, "uncertain"
	}
	var envelope struct {
		Success *bool             `json:"success"`
		Result  *[]CompanyProfile `json:"result"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Success == nil || !*envelope.Success || envelope.Result == nil {
		return CompanyWriteResult{}, "uncertain"
	}
	if len(*envelope.Result) == 0 {
		return CompanyWriteResult{}, "absent"
	}
	if len(*envelope.Result) != 1 || (*envelope.Result)[0].ID <= 0 || (*envelope.Result)[0].Company != input.Company {
		return CompanyWriteResult{}, "uncertain"
	}
	return CompanyWriteResult{ID: (*envelope.Result)[0].ID, Status: "created"}, "found"
}

func (dispatch *createCompanyDispatch) execute(ctx context.Context) error {
	dispatch.response = dispatch.client.requestLeadAPIWithContext(ctx, dispatch.call, http.MethodPost, "/v1/companies.json", dispatch.body)
	return nil
}

func createCompanyUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[CompanyWriteResult] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "adobe-marketo", Operation: "createCompany", Message: message}
	return sdkgo.NewMutationUncertain(CompanyWriteResult{}, failure, companyReceipt(call, 0))
}
