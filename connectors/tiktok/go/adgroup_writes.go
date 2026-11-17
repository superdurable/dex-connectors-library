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

// CreateAdgroupInput creates one regular ad group.
type CreateAdgroupInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// CampaignID is the parent campaign ID.
	CampaignID string `json:"campaignId"`
	// Name is the ad group display name.
	Name string `json:"name"`
	// BillingEvent is TikTok's billing event.
	BillingEvent string `json:"billingEvent"`
	// Budget is the ad group budget in advertiser currency.
	Budget float64 `json:"budget"`
	// BudgetMode is the TikTok budget mode.
	BudgetMode string `json:"budgetMode"`
	// OptimizationGoal is TikTok's optimization goal.
	OptimizationGoal string `json:"optimizationGoal"`
	// Pacing is TikTok's delivery pacing mode.
	Pacing string `json:"pacing"`
	// ScheduleStartTime is the provider-formatted start time.
	ScheduleStartTime string `json:"scheduleStartTime"`
	// ScheduleEndTime is the optional provider-formatted end time.
	ScheduleEndTime string `json:"scheduleEndTime,omitempty"`
	// ScheduleType is TikTok's delivery schedule type.
	ScheduleType string `json:"scheduleType"`
	// LocationIDs are optional TikTok target region IDs.
	LocationIDs []string `json:"locationIds,omitempty"`
}

// UpdateAdgroupInput replaces selected ad group settings.
type UpdateAdgroupInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// AdgroupID is the decimal ad group ID.
	AdgroupID string `json:"adgroupId"`
	// Name is an optional replacement display name; nil leaves it unchanged.
	Name *string `json:"name,omitempty"`
	// Budget is an optional replacement amount in advertiser currency; nil leaves it unchanged.
	Budget *float64 `json:"budget,omitempty"`
}

// AdgroupWriteResult identifies a confirmed ad group write.
type AdgroupWriteResult struct {
	// ID is the created or updated ad group ID.
	ID string `json:"id"`
}

// CreateAdgroupOperation creates one adgroup under a limited resend cap.
type CreateAdgroupOperation struct{ client *Client }

// CreateAdgroup binds the mutation to this client.
func (client *Client) CreateAdgroup() CreateAdgroupOperation {
	return CreateAdgroupOperation{client}
}

// Definition returns the generated adgroup create contract.
func (CreateAdgroupOperation) Definition() sdkgo.MutationDefinition { return CreateAdgroupDefinition }

// IdempotencyKey derives stable Dex identity; TikTok does not deduplicate regular adgroup creates.
func (CreateAdgroupOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAdgroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends at most the configured number of creates and preserves uncertainty after an unconfirmed cap.
func (operation CreateAdgroupOperation) Invoke(call sdkgo.Call, input CreateAdgroupInput) sdkgo.MutationAttempt[AdgroupWriteResult] {
	if !validDecimalID(input.AdvertiserID) || !validDecimalID(input.CampaignID) || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BillingEvent) == "" || input.Budget <= 0 || strings.TrimSpace(input.BudgetMode) == "" || strings.TrimSpace(input.OptimizationGoal) == "" || strings.TrimSpace(input.Pacing) == "" || strings.TrimSpace(input.ScheduleStartTime) == "" || strings.TrimSpace(input.ScheduleType) == "" {
		return adgroupWriteBranch(CreateAdgroupBranchDefect, "createAdgroup", sdkgo.FailureValidation, "adgroup create input invalid")
	}
	for _, locationID := range input.LocationIDs {
		if !validDecimalID(locationID) {
			return adgroupWriteBranch(CreateAdgroupBranchDefect, "createAdgroup", sdkgo.FailureValidation, "location ID invalid")
		}
	}
	payload := map[string]any{"advertiser_id": input.AdvertiserID, "campaign_id": input.CampaignID, "adgroup_name": input.Name, "billing_event": input.BillingEvent, "budget": input.Budget, "budget_mode": input.BudgetMode, "optimization_goal": input.OptimizationGoal, "pacing": input.Pacing, "schedule_start_time": input.ScheduleStartTime, "schedule_type": input.ScheduleType}
	if input.ScheduleEndTime != "" {
		payload["schedule_end_time"] = input.ScheduleEndTime
	}
	if len(input.LocationIDs) > 0 {
		payload["location_ids"] = input.LocationIDs
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return adgroupWriteBranch(CreateAdgroupBranchDefect, "createAdgroup", sdkgo.FailureLocalDefect, "adgroup input could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateAdgroupDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return adgroupWriteBranch(CreateAdgroupBranchDefect, "createAdgroup", sdkgo.FailureLocalDefect, "adgroup create checkpoint unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return adgroupWriteBranch(CreateAdgroupBranchDefect, "createAdgroup", sdkgo.FailureLocalDefect, "adgroup create checkpoint invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertainAdgroupCreate(sdkgo.FailureAvailability, "adgroup create could not be confirmed")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.postWithContext(call, requestContext, "/adgroup/create/", body)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("createAdgroup", sdkgo.FailureTransport, "adgroup dispatch was not started"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("createAdgroup", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
	}
	if errors.Is(response.err, errCredential) {
		return adgroupWriteBranch(CreateAdgroupBranchDefect, "createAdgroup", sdkgo.FailureAuthentication, "TikTok credentials unavailable")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("createAdgroup", sdkgo.FailureRateLimit, "TikTok rate limited adgroup create"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("createAdgroup", sdkgo.FailureAvailability, "TikTok did not process adgroup create"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return retryOrUncertainAdgroupCreate(call, decision.MaximumPriorSends, sdkgo.FailureAvailability, "adgroup create outcome unknown", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return adgroupWriteBranch(CreateAdgroupBranchProviderRejected, "createAdgroup", sdkgo.FailureProviderRejection, "TikTok rejected HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Code *int `json:"code"`
		Data struct {
			AdgroupID string `json:"adgroup_id"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Code == nil {
		return retryOrUncertainAdgroupCreate(call, decision.MaximumPriorSends, sdkgo.FailureProtocol, "adgroup create response invalid", 0)
	}
	if *envelope.Code == tiktokCodeRateLimited {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("createAdgroup", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("createAdgroup", sdkgo.FailureRateLimit, "TikTok rate limited adgroup create"), response.retryAfter)
	}
	if isTikTokSystemErrorCode(*envelope.Code) {
		return retryOrUncertainAdgroupCreate(call, decision.MaximumPriorSends, sdkgo.FailureAvailability, "adgroup create outcome unknown", response.retryAfter)
	}
	if *envelope.Code != 0 {
		return adgroupWriteBranch(CreateAdgroupBranchProviderRejected, "createAdgroup", sdkgo.FailureProviderRejection, "TikTok rejected adgroup create with code "+strconv.Itoa(*envelope.Code))
	}
	if !validDecimalID(envelope.Data.AdgroupID) {
		return retryOrUncertainAdgroupCreate(call, decision.MaximumPriorSends, sdkgo.FailureProtocol, "adgroup ID was not confirmed", 0)
	}
	_ = fence.RecordProviderID(envelope.Data.AdgroupID)
	return sdkgo.NewMutationBranch(CreateAdgroupBranchCreated, AdgroupWriteResult{ID: envelope.Data.AdgroupID}, nil, sdkgo.Receipt{})
}

// UpdateAdgroupOperation replaces a adgroup name or budget.
type UpdateAdgroupOperation struct{ client *Client }

// UpdateAdgroup binds the mutation to this client.
func (client *Client) UpdateAdgroup() UpdateAdgroupOperation {
	return UpdateAdgroupOperation{client}
}

// Definition returns the generated adgroup update contract.
func (UpdateAdgroupOperation) Definition() sdkgo.MutationDefinition { return UpdateAdgroupDefinition }

// IdempotencyKey derives stable Dex identity for an absolute adgroup update.
func (UpdateAdgroupOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAdgroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected adgroup settings and repeats unknown outcomes safely.
func (operation UpdateAdgroupOperation) Invoke(call sdkgo.Call, input UpdateAdgroupInput) sdkgo.MutationAttempt[AdgroupWriteResult] {
	if !validDecimalID(input.AdvertiserID) || !validDecimalID(input.AdgroupID) || (input.Name == nil && input.Budget == nil) || (input.Name != nil && strings.TrimSpace(*input.Name) == "") || (input.Budget != nil && *input.Budget <= 0) {
		return adgroupWriteBranch(UpdateAdgroupBranchDefect, "updateAdgroup", sdkgo.FailureValidation, "adgroup update input invalid")
	}
	payload := map[string]any{"advertiser_id": input.AdvertiserID, "adgroup_id": input.AdgroupID}
	if input.Name != nil {
		payload["adgroup_name"] = *input.Name
	}
	if input.Budget != nil {
		payload["budget"] = *input.Budget
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return adgroupWriteBranch(UpdateAdgroupBranchDefect, "updateAdgroup", sdkgo.FailureLocalDefect, "adgroup update could not be encoded")
	}
	response := operation.client.post(call, "/adgroup/update/", body)
	if errors.Is(response.err, errCredential) {
		return adgroupWriteBranch(UpdateAdgroupBranchDefect, "updateAdgroup", sdkgo.FailureAuthentication, "TikTok credentials unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("updateAdgroup", sdkgo.FailureTransport, "TikTok request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("updateAdgroup", sdkgo.FailureRateLimit, "TikTok rate limited update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("updateAdgroup", sdkgo.FailureAvailability, "TikTok update outcome unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return adgroupWriteBranch(UpdateAdgroupBranchProviderRejected, "updateAdgroup", sdkgo.FailureProviderRejection, "TikTok rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return adgroupWriteBranch(UpdateAdgroupBranchInvalidResponse, "updateAdgroup", sdkgo.FailureResponseTooLarge, "TikTok response exceeded limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("updateAdgroup", sdkgo.FailureTransport, "TikTok response interrupted"), 0)
	}
	var envelope struct {
		Code *int `json:"code"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Code == nil {
		return adgroupWriteBranch(UpdateAdgroupBranchInvalidResponse, "updateAdgroup", sdkgo.FailureProtocol, "TikTok update JSON invalid")
	}
	if *envelope.Code == tiktokCodeRateLimited {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("updateAdgroup", sdkgo.FailureRateLimit, "TikTok rate limited update"), response.retryAfter)
	}
	if isTikTokSystemErrorCode(*envelope.Code) {
		return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("updateAdgroup", sdkgo.FailureAvailability, "TikTok update outcome unknown"), response.retryAfter)
	}
	if *envelope.Code != 0 {
		return adgroupWriteBranch(UpdateAdgroupBranchProviderRejected, "updateAdgroup", sdkgo.FailureProviderRejection, "TikTok rejected update with code "+strconv.Itoa(*envelope.Code))
	}
	return sdkgo.NewMutationBranch(UpdateAdgroupBranchUpdated, AdgroupWriteResult{ID: input.AdgroupID}, nil, sdkgo.Receipt{})
}

func adgroupWriteFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "tiktok-business", Operation: operation, Message: message}
}

func adgroupWriteBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AdgroupWriteResult] {
	failure := adgroupWriteFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, AdgroupWriteResult{}, &failure, sdkgo.Receipt{})
}

func uncertainAdgroupCreate(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AdgroupWriteResult] {
	return sdkgo.NewMutationUncertain(AdgroupWriteResult{}, adgroupWriteFailure("createAdgroup", kind, message), sdkgo.Receipt{})
}

func retryOrUncertainAdgroupCreate(call sdkgo.Call, priorSends int32, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[AdgroupWriteResult] {
	if priorSends+1 >= call.MaxSends() {
		return uncertainAdgroupCreate(kind, message)
	}
	return sdkgo.NewMutationRetry[AdgroupWriteResult](adgroupWriteFailure("createAdgroup", kind, message), retryAfter)
}
