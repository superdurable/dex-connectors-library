// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiktok

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateCampaignStatusInput sets one campaign's operation status.
type UpdateCampaignStatusInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// CampaignID is the decimal campaign ID.
	CampaignID string `json:"campaignId"`
	// OperationStatus is ENABLE, DISABLE, or DELETE.
	OperationStatus string `json:"operationStatus"`
}

// UpdateAdgroupStatusInput sets one ad group's operation status.
type UpdateAdgroupStatusInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// AdgroupID is the decimal ad group ID.
	AdgroupID string `json:"adgroupId"`
	// OperationStatus is ENABLE, DISABLE, or DELETE.
	OperationStatus string `json:"operationStatus"`
}

// UpdateAdStatusInput sets one ad's operation status.
type UpdateAdStatusInput struct {
	// AdvertiserID is the decimal ad account ID.
	AdvertiserID string `json:"advertiserId"`
	// AdID is the decimal ad ID.
	AdID string `json:"adId"`
	// OperationStatus is ENABLE, DISABLE, or DELETE.
	OperationStatus string `json:"operationStatus"`
}

// AdvertisingStatusResult identifies a confirmed status change without account details.
type AdvertisingStatusResult struct {
	// ResourceID is the selected campaign, ad group, or ad ID.
	ResourceID string `json:"resourceId"`
	// OperationStatus is the requested final status.
	OperationStatus string `json:"operationStatus"`
}

// UpdateCampaignStatusOperation changes one campaign status.
type UpdateCampaignStatusOperation struct{ client *Client }

// UpdateCampaignStatus binds the mutation to this client.
func (client *Client) UpdateCampaignStatus() UpdateCampaignStatusOperation {
	return UpdateCampaignStatusOperation{client}
}

// Definition returns the generated campaign status contract.
func (UpdateCampaignStatusOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCampaignStatusDefinition
}

// IdempotencyKey derives stable Dex identity for an absolute status replacement.
func (UpdateCampaignStatusOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCampaignStatusInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets one campaign status and repeats unknown outcomes safely.
func (operation UpdateCampaignStatusOperation) Invoke(call sdkgo.Call, input UpdateCampaignStatusInput) sdkgo.MutationAttempt[AdvertisingStatusResult] {
	return invokeAdvertisingStatus(call, operation.client, "campaign", "campaign_ids", "updateCampaignStatus", input.AdvertiserID, input.CampaignID, input.OperationStatus, UpdateCampaignStatusBranchUpdated, UpdateCampaignStatusBranchProviderRejected, UpdateCampaignStatusBranchInvalidResponse, UpdateCampaignStatusBranchDefect)
}

// UpdateAdgroupStatusOperation changes one ad group status.
type UpdateAdgroupStatusOperation struct{ client *Client }

// UpdateAdgroupStatus binds the mutation to this client.
func (client *Client) UpdateAdgroupStatus() UpdateAdgroupStatusOperation {
	return UpdateAdgroupStatusOperation{client}
}

// Definition returns the generated ad group status contract.
func (UpdateAdgroupStatusOperation) Definition() sdkgo.MutationDefinition {
	return UpdateAdgroupStatusDefinition
}

// IdempotencyKey derives stable Dex identity for an absolute status replacement.
func (UpdateAdgroupStatusOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAdgroupStatusInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets one ad group status and repeats unknown outcomes safely.
func (operation UpdateAdgroupStatusOperation) Invoke(call sdkgo.Call, input UpdateAdgroupStatusInput) sdkgo.MutationAttempt[AdvertisingStatusResult] {
	return invokeAdvertisingStatus(call, operation.client, "adgroup", "adgroup_ids", "updateAdgroupStatus", input.AdvertiserID, input.AdgroupID, input.OperationStatus, UpdateAdgroupStatusBranchUpdated, UpdateAdgroupStatusBranchProviderRejected, UpdateAdgroupStatusBranchInvalidResponse, UpdateAdgroupStatusBranchDefect)
}

// UpdateAdStatusOperation changes one ad status.
type UpdateAdStatusOperation struct{ client *Client }

// UpdateAdStatus binds the mutation to this client.
func (client *Client) UpdateAdStatus() UpdateAdStatusOperation {
	return UpdateAdStatusOperation{client}
}

// Definition returns the generated ad status contract.
func (UpdateAdStatusOperation) Definition() sdkgo.MutationDefinition { return UpdateAdStatusDefinition }

// IdempotencyKey derives stable Dex identity for an absolute status replacement.
func (UpdateAdStatusOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAdStatusInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets one ad status and repeats unknown outcomes safely.
func (operation UpdateAdStatusOperation) Invoke(call sdkgo.Call, input UpdateAdStatusInput) sdkgo.MutationAttempt[AdvertisingStatusResult] {
	return invokeAdvertisingStatus(call, operation.client, "ad", "ad_ids", "updateAdStatus", input.AdvertiserID, input.AdID, input.OperationStatus, UpdateAdStatusBranchUpdated, UpdateAdStatusBranchProviderRejected, UpdateAdStatusBranchInvalidResponse, UpdateAdStatusBranchDefect)
}

func invokeAdvertisingStatus(call sdkgo.Call, client *Client, resource, identifierField, operation, advertiserID, resourceID, operationStatus string, updated, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[AdvertisingStatusResult] {
	branch := func(branchID sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AdvertisingStatusResult] {
		failure := sdkgo.Failure{Kind: kind, Provider: "tiktok-business", Operation: operation, Message: message}
		return sdkgo.NewMutationBranch(branchID, AdvertisingStatusResult{}, &failure, sdkgo.Receipt{})
	}
	retry := func(kind sdkgo.FailureKind, message string, response providerResponse) sdkgo.MutationAttempt[AdvertisingStatusResult] {
		failure := sdkgo.Failure{Kind: kind, Provider: "tiktok-business", Operation: operation, Message: message}
		return sdkgo.NewMutationRetry[AdvertisingStatusResult](failure, response.retryAfter)
	}
	if !validDecimalID(advertiserID) || !validDecimalID(resourceID) || (operationStatus != "ENABLE" && operationStatus != "DISABLE" && operationStatus != "DELETE") {
		return branch(defect, sdkgo.FailureValidation, "advertiser ID, resource ID, or operation status is invalid")
	}
	payload, err := json.Marshal(map[string]any{"advertiser_id": advertiserID, identifierField: []string{resourceID}, "operation_status": operationStatus})
	if err != nil {
		return branch(defect, sdkgo.FailureLocalDefect, "status request cannot be encoded")
	}
	response := client.post(call, "/"+resource+"/status/update/", payload)
	if errors.Is(response.err, errCredential) {
		return branch(defect, sdkgo.FailureAuthentication, "TikTok credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return retry(sdkgo.FailureTransport, "TikTok request failed", response)
	}
	if response.status == http.StatusTooManyRequests {
		return retry(sdkgo.FailureRateLimit, "TikTok rate limited the request", response)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return retry(sdkgo.FailureAvailability, "TikTok status outcome is unknown", response)
	}
	if response.status != http.StatusOK {
		return branch(rejected, sdkgo.FailureProviderRejection, "TikTok rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return branch(invalid, sdkgo.FailureResponseTooLarge, "TikTok response exceeded the configured limit")
	}
	if response.err != nil {
		return retry(sdkgo.FailureTransport, "TikTok response was interrupted", response)
	}
	var envelope struct {
		Code *int `json:"code"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Code == nil {
		return branch(invalid, sdkgo.FailureProtocol, "TikTok returned invalid status JSON")
	}
	if *envelope.Code == tiktokCodeRateLimited {
		return retry(sdkgo.FailureRateLimit, "TikTok rate limited the request", response)
	}
	if isTikTokSystemErrorCode(*envelope.Code) {
		return retry(sdkgo.FailureAvailability, "TikTok status outcome is unknown", response)
	}
	if *envelope.Code != 0 {
		return branch(rejected, sdkgo.FailureProviderRejection, "TikTok rejected status with code "+strconv.Itoa(*envelope.Code))
	}
	return sdkgo.NewMutationBranch(updated, AdvertisingStatusResult{ResourceID: resourceID, OperationStatus: operationStatus}, nil, sdkgo.Receipt{})
}
