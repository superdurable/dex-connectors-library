// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateCampaignInput creates one regular campaign with the minimum provider fields.
type CreateCampaignInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// Name is the campaign display name.
	Name string `json:"name"`
	// ObjectiveType is a TikTok objective such as LEAD_GENERATION.
	ObjectiveType string `json:"objectiveType"`
	// BudgetMode is a TikTok mode such as BUDGET_MODE_TOTAL; blank omits campaign-level budgeting.
	BudgetMode string `json:"budgetMode,omitempty"`
	// Budget is the campaign budget in advertiser currency; zero omits it.
	Budget float64 `json:"budget,omitempty"`
}

// UpdateCampaignInput replaces selected campaign settings.
type UpdateCampaignInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// CampaignID is the decimal campaign ID.
	CampaignID string `json:"campaignId"`
	// Name is an optional replacement display name; nil leaves it unchanged.
	Name *string `json:"name,omitempty"`
	// Budget is an optional replacement amount in advertiser currency; nil leaves it unchanged.
	Budget *float64 `json:"budget,omitempty"`
}

// CampaignWriteResult identifies a confirmed campaign write without account details.
type CampaignWriteResult struct {
	// ID is the created or updated campaign ID.
	ID string `json:"id"`
}

// CreateCampaignOperation creates one campaign under a limited resend cap.
type CreateCampaignOperation struct{ client *Client }

// CreateCampaign binds the mutation to this client.
func (client *Client) CreateCampaign() CreateCampaignOperation {
	return CreateCampaignOperation{client}
}

// Definition returns the generated campaign create contract.
func (CreateCampaignOperation) Definition() sdkgo.MutationDefinition { return CreateCampaignDefinition }

// IdempotencyKey derives stable Dex identity; TikTok does not deduplicate regular campaign creates.
func (CreateCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends at most the configured number of creates and preserves uncertainty after an unconfirmed cap.
func (operation CreateCampaignOperation) Invoke(call sdkgo.Call, input CreateCampaignInput) sdkgo.MutationAttempt[CampaignWriteResult] {
	if !validDecimalID(input.AdvertiserID) || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.ObjectiveType) == "" || input.Budget < 0 || (input.Budget > 0 && input.BudgetMode == "") {
		return campaignWriteBranch(CreateCampaignBranchDefect, "createCampaign", sdkgo.FailureValidation, "campaign create input invalid")
	}
	payload := map[string]any{"advertiser_id": input.AdvertiserID, "campaign_name": input.Name, "objective_type": input.ObjectiveType}
	if input.BudgetMode != "" {
		payload["budget_mode"] = input.BudgetMode
	}
	if input.Budget > 0 {
		payload["budget"] = input.Budget
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return campaignWriteBranch(CreateCampaignBranchDefect, "createCampaign", sdkgo.FailureLocalDefect, "campaign input could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateCampaignDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return campaignWriteBranch(CreateCampaignBranchDefect, "createCampaign", sdkgo.FailureLocalDefect, "campaign create checkpoint unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return campaignWriteBranch(CreateCampaignBranchDefect, "createCampaign", sdkgo.FailureLocalDefect, "campaign create checkpoint invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertainCampaignCreate(sdkgo.FailureAvailability, "campaign create could not be confirmed")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.postWithContext(call, requestContext, "/campaign/create/", body)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("createCampaign", sdkgo.FailureTransport, "campaign dispatch was not started"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("createCampaign", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
	}
	if errors.Is(response.err, errCredential) {
		return campaignWriteBranch(CreateCampaignBranchDefect, "createCampaign", sdkgo.FailureAuthentication, "TikTok credentials unavailable")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("createCampaign", sdkgo.FailureRateLimit, "TikTok rate limited campaign create"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("createCampaign", sdkgo.FailureAvailability, "TikTok did not process campaign create"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return retryOrUncertainCampaignCreate(call, decision.MaximumPriorSends, sdkgo.FailureAvailability, "campaign create outcome unknown", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return campaignWriteBranch(CreateCampaignBranchProviderRejected, "createCampaign", sdkgo.FailureProviderRejection, "TikTok rejected HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Code *int `json:"code"`
		Data struct {
			CampaignID string `json:"campaign_id"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Code == nil {
		return retryOrUncertainCampaignCreate(call, decision.MaximumPriorSends, sdkgo.FailureProtocol, "campaign create response invalid", 0)
	}
	if *envelope.Code == tiktokCodeRateLimited {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("createCampaign", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("createCampaign", sdkgo.FailureRateLimit, "TikTok rate limited campaign create"), response.retryAfter)
	}
	if isTikTokSystemErrorCode(*envelope.Code) {
		return retryOrUncertainCampaignCreate(call, decision.MaximumPriorSends, sdkgo.FailureAvailability, "campaign create outcome unknown", response.retryAfter)
	}
	if *envelope.Code != 0 {
		return campaignWriteBranch(CreateCampaignBranchProviderRejected, "createCampaign", sdkgo.FailureProviderRejection, "TikTok rejected campaign create with code "+strconv.Itoa(*envelope.Code))
	}
	if !validDecimalID(envelope.Data.CampaignID) {
		return retryOrUncertainCampaignCreate(call, decision.MaximumPriorSends, sdkgo.FailureProtocol, "campaign ID was not confirmed", 0)
	}
	_ = fence.RecordProviderID(envelope.Data.CampaignID)
	return sdkgo.NewMutationBranch(CreateCampaignBranchCreated, CampaignWriteResult{ID: envelope.Data.CampaignID}, nil, sdkgo.Receipt{})
}

// UpdateCampaignOperation replaces a campaign name or budget.
type UpdateCampaignOperation struct{ client *Client }

// UpdateCampaign binds the mutation to this client.
func (client *Client) UpdateCampaign() UpdateCampaignOperation {
	return UpdateCampaignOperation{client}
}

// Definition returns the generated campaign update contract.
func (UpdateCampaignOperation) Definition() sdkgo.MutationDefinition { return UpdateCampaignDefinition }

// IdempotencyKey derives stable Dex identity for an absolute campaign update.
func (UpdateCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected campaign settings and repeats unknown outcomes safely.
func (operation UpdateCampaignOperation) Invoke(call sdkgo.Call, input UpdateCampaignInput) sdkgo.MutationAttempt[CampaignWriteResult] {
	if !validDecimalID(input.AdvertiserID) || !validDecimalID(input.CampaignID) || (input.Name == nil && input.Budget == nil) || (input.Name != nil && strings.TrimSpace(*input.Name) == "") || (input.Budget != nil && *input.Budget <= 0) {
		return campaignWriteBranch(UpdateCampaignBranchDefect, "updateCampaign", sdkgo.FailureValidation, "campaign update input invalid")
	}
	payload := map[string]any{"advertiser_id": input.AdvertiserID, "campaign_id": input.CampaignID}
	if input.Name != nil {
		payload["campaign_name"] = *input.Name
	}
	if input.Budget != nil {
		payload["budget"] = *input.Budget
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return campaignWriteBranch(UpdateCampaignBranchDefect, "updateCampaign", sdkgo.FailureLocalDefect, "campaign update could not be encoded")
	}
	response := operation.client.post(call, "/campaign/update/", body)
	if errors.Is(response.err, errCredential) {
		return campaignWriteBranch(UpdateCampaignBranchDefect, "updateCampaign", sdkgo.FailureAuthentication, "TikTok credentials unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("updateCampaign", sdkgo.FailureTransport, "TikTok request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("updateCampaign", sdkgo.FailureRateLimit, "TikTok rate limited update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("updateCampaign", sdkgo.FailureAvailability, "TikTok update outcome unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return campaignWriteBranch(UpdateCampaignBranchProviderRejected, "updateCampaign", sdkgo.FailureProviderRejection, "TikTok rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return campaignWriteBranch(UpdateCampaignBranchInvalidResponse, "updateCampaign", sdkgo.FailureResponseTooLarge, "TikTok response exceeded limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("updateCampaign", sdkgo.FailureTransport, "TikTok response interrupted"), 0)
	}
	var envelope struct {
		Code *int `json:"code"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Code == nil {
		return campaignWriteBranch(UpdateCampaignBranchInvalidResponse, "updateCampaign", sdkgo.FailureProtocol, "TikTok update JSON invalid")
	}
	if *envelope.Code == tiktokCodeRateLimited {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("updateCampaign", sdkgo.FailureRateLimit, "TikTok rate limited update"), response.retryAfter)
	}
	if isTikTokSystemErrorCode(*envelope.Code) {
		return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("updateCampaign", sdkgo.FailureAvailability, "TikTok update outcome unknown"), response.retryAfter)
	}
	if *envelope.Code != 0 {
		return campaignWriteBranch(UpdateCampaignBranchProviderRejected, "updateCampaign", sdkgo.FailureProviderRejection, "TikTok rejected update with code "+strconv.Itoa(*envelope.Code))
	}
	return sdkgo.NewMutationBranch(UpdateCampaignBranchUpdated, CampaignWriteResult{ID: input.CampaignID}, nil, sdkgo.Receipt{})
}

func campaignWriteFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "tiktok-business", Operation: operation, Message: message}
}

func campaignWriteBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CampaignWriteResult] {
	failure := campaignWriteFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, CampaignWriteResult{}, &failure, sdkgo.Receipt{})
}

func uncertainCampaignCreate(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CampaignWriteResult] {
	return sdkgo.NewMutationUncertain(CampaignWriteResult{}, campaignWriteFailure("createCampaign", kind, message), sdkgo.Receipt{})
}

func retryOrUncertainCampaignCreate(call sdkgo.Call, priorSends int32, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CampaignWriteResult] {
	if priorSends+1 >= call.MaxSends() {
		return uncertainCampaignCreate(kind, message)
	}
	return sdkgo.NewMutationRetry[CampaignWriteResult](campaignWriteFailure("createCampaign", kind, message), retryAfter)
}
