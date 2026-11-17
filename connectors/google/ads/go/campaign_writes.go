// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CampaignMutationResult identifies the campaign changed by a mutate call.
type CampaignMutationResult struct {
	// ResourceName is customers/{customer}/campaigns/{campaign}.
	ResourceName string `json:"resourceName"`
}

// CreateCampaignInput creates a paused Search campaign using an existing budget.
type CreateCampaignInput struct {
	// CustomerID is the unhyphenated numeric client account ID.
	CustomerID string `json:"customerId"`
	// ManagerCustomerID is the unhyphenated manager ID when needed; blank omits the header.
	ManagerCustomerID string `json:"managerCustomerId,omitempty"`
	// Name is a nonblank campaign name, at most 255 characters.
	Name string `json:"name"`
	// CampaignBudget is an existing customers/{customer}/campaignBudgets/{budget} resource name.
	CampaignBudget string `json:"campaignBudget"`
	// ContainsEUPoliticalAdvertising is DOES_NOT_CONTAIN_EU_POLITICAL_ADVERTISING or CONTAINS_EU_POLITICAL_ADVERTISING.
	ContainsEUPoliticalAdvertising string `json:"containsEuPoliticalAdvertising"`
}

// UpdateCampaignStatusInput sets an absolute serving status.
type UpdateCampaignStatusInput struct {
	// CustomerID is the unhyphenated numeric client account ID.
	CustomerID string `json:"customerId"`
	// ManagerCustomerID is the unhyphenated manager ID when needed; blank omits the header.
	ManagerCustomerID string `json:"managerCustomerId,omitempty"`
	// CampaignID is the numeric campaign ID.
	CampaignID string `json:"campaignId"`
	// Status is ENABLED or PAUSED; ENABLED can begin spending the linked budget.
	Status string `json:"status"`
}

// CreateCampaignOperation creates a paused campaign with bounded sends.
type CreateCampaignOperation struct{ client *Client }

// UpdateCampaignStatusOperation replaces campaign serving status.
type UpdateCampaignStatusOperation struct{ client *Client }

// RemoveCampaignOperation removes a campaign.
type RemoveCampaignOperation struct{ client *Client }

// CreateCampaign binds the campaign creation Mutation to this client.
func (client *Client) CreateCampaign() CreateCampaignOperation {
	return CreateCampaignOperation{client}
}

// UpdateCampaignStatus binds the status Mutation to this client.
func (client *Client) UpdateCampaignStatus() UpdateCampaignStatusOperation {
	return UpdateCampaignStatusOperation{client}
}

// RemoveCampaign binds the removal Mutation to this client.
func (client *Client) RemoveCampaign() RemoveCampaignOperation {
	return RemoveCampaignOperation{client}
}

// Definition returns the generated creation contract.
func (CreateCampaignOperation) Definition() sdkgo.MutationDefinition { return CreateCampaignDefinition }

// Definition returns the generated status update contract.
func (UpdateCampaignStatusOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCampaignStatusDefinition
}

// Definition returns the generated removal contract.
func (RemoveCampaignOperation) Definition() sdkgo.MutationDefinition { return RemoveCampaignDefinition }

// IdempotencyKey derives a stable Dex call identity.
func (CreateCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable Dex call identity.
func (UpdateCampaignStatusOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCampaignStatusInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable Dex call identity.
func (RemoveCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ GetCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

var budgetNamePattern = regexp.MustCompile(`^customers/([0-9]{1,20})/campaignBudgets/[0-9]{1,20}$`)

// Invoke creates a PAUSED campaign and caps possible duplicate sends.
func (operation CreateCampaignOperation) Invoke(call sdkgo.Call, input CreateCampaignInput) sdkgo.MutationAttempt[CampaignMutationResult] {
	budgetMatch := budgetNamePattern.FindStringSubmatch(input.CampaignBudget)
	if !validAdsIDs(input.CustomerID, input.ManagerCustomerID) || budgetMatch == nil || budgetMatch[1] != input.CustomerID || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || (input.ContainsEUPoliticalAdvertising != "DOES_NOT_CONTAIN_EU_POLITICAL_ADVERTISING" && input.ContainsEUPoliticalAdvertising != "CONTAINS_EU_POLITICAL_ADVERTISING") {
		return campaignMutationBranch(CreateCampaignBranchDefect, "createCampaign", sdkgo.FailureValidation, "campaign identity, budget, name, or EU declaration is invalid")
	}
	create := map[string]any{"name": input.Name, "campaignBudget": input.CampaignBudget, "advertisingChannelType": "SEARCH", "status": "PAUSED", "manualCpc": map[string]any{}, "containsEuPoliticalAdvertising": input.ContainsEUPoliticalAdvertising}
	body, _ := json.Marshal(map[string]any{"operations": []any{map[string]any{"create": create}}})
	fence, err := singledispatch.NewFence(call, 30*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return campaignMutationBranch(CreateCampaignBranchDefect, "createCampaign", sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return uncertainCampaign(call, "a previous create cannot be confirmed")
	}
	var response providerResponse
	err = fence.Dispatch(func(ctx context.Context) error {
		response = operation.client.postInContext(call, ctx, campaignMutatePath(input.CustomerID), body, input.ManagerCustomerID)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CampaignMutationResult](adsFailure("createCampaign", sdkgo.FailureAvailability, "campaign write was not dispatched"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return uncertainCampaign(call, "a previous create may have succeeded")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[CampaignMutationResult](adsFailure("createCampaign", sdkgo.FailureAvailability, "credential checkpoint is unavailable"), 0)
		}
		return campaignMutationBranch(CreateCampaignBranchDefect, "createCampaign", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[CampaignMutationResult](adsFailure("createCampaign", sdkgo.FailureAvailability, "retry checkpoint is unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CampaignMutationResult](adsFailure("createCampaign", kind, "Google Ads asked to retry"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[CampaignMutationResult](adsFailure("createCampaign", sdkgo.FailureAvailability, "campaign create outcome is unknown"), response.retryAfter)
		}
		return uncertainCampaign(call, "campaign create outcome is unknown after the send limit")
	}
	if response.status == http.StatusOK {
		result, decodeErr := decodeCampaignMutation(response.body)
		if decodeErr != nil {
			return uncertainCampaign(call, "Google Ads accepted a campaign but returned invalid JSON")
		}
		_ = fence.RecordProviderID(result.ResourceName)
		return sdkgo.NewMutationBranch(CreateCampaignBranchCreated, result, nil, campaignReceipt(call, result.ResourceName))
	}
	if decision.MaximumPriorSends > 0 {
		return uncertainCampaign(call, "a previous create may have succeeded")
	}
	return campaignMutationBranch(CreateCampaignBranchProviderRejected, "createCampaign", sdkgo.FailureProviderRejection, "Google Ads rejected HTTP "+strconv.Itoa(response.status))
}

// Invoke replaces a campaign's serving status with ENABLED or PAUSED.
func (operation UpdateCampaignStatusOperation) Invoke(call sdkgo.Call, input UpdateCampaignStatusInput) sdkgo.MutationAttempt[CampaignMutationResult] {
	if !validAdsIDs(input.CustomerID, input.ManagerCustomerID) || !numericAdsID.MatchString(input.CampaignID) || (input.Status != "ENABLED" && input.Status != "PAUSED") {
		return campaignMutationBranch(UpdateCampaignStatusBranchDefect, "updateCampaignStatus", sdkgo.FailureValidation, "campaign identity or status is invalid")
	}
	resourceName := campaignResourceName(input.CustomerID, input.CampaignID)
	body, _ := json.Marshal(map[string]any{"operations": []any{map[string]any{"updateMask": "status", "update": map[string]string{"resourceName": resourceName, "status": input.Status}}}})
	return safeCampaignMutation(operation.client.post(call, campaignMutatePath(input.CustomerID), body, input.ManagerCustomerID), call, "updateCampaignStatus", UpdateCampaignStatusBranchUpdated, UpdateCampaignStatusBranchProviderRejected, UpdateCampaignStatusBranchInvalidResponse, UpdateCampaignStatusBranchDefect)
}

// Invoke marks a campaign REMOVED through the provider's mutate method.
func (operation RemoveCampaignOperation) Invoke(call sdkgo.Call, input GetCampaignInput) sdkgo.MutationAttempt[CampaignMutationResult] {
	if !validAdsIDs(input.CustomerID, input.ManagerCustomerID) || !numericAdsID.MatchString(input.CampaignID) {
		return campaignMutationBranch(RemoveCampaignBranchDefect, "removeCampaign", sdkgo.FailureValidation, "campaign identity is invalid")
	}
	resourceName := campaignResourceName(input.CustomerID, input.CampaignID)
	body, _ := json.Marshal(map[string]any{"operations": []any{map[string]string{"remove": resourceName}}})
	return safeCampaignMutation(operation.client.post(call, campaignMutatePath(input.CustomerID), body, input.ManagerCustomerID), call, "removeCampaign", RemoveCampaignBranchRemoved, RemoveCampaignBranchProviderRejected, RemoveCampaignBranchInvalidResponse, RemoveCampaignBranchDefect)
}

func campaignMutatePath(customerID string) string {
	return "/v26/customers/" + customerID + "/campaigns:mutate"
}

func campaignResourceName(customerID, campaignID string) string {
	return "customers/" + customerID + "/campaigns/" + campaignID
}

func decodeCampaignMutation(body []byte) (CampaignMutationResult, error) {
	var response struct {
		Results             []CampaignMutationResult `json:"results"`
		PartialFailureError json.RawMessage          `json:"partialFailureError"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return CampaignMutationResult{}, err
	}
	if len(response.PartialFailureError) > 0 || len(response.Results) != 1 || !strings.HasPrefix(response.Results[0].ResourceName, "customers/") {
		return CampaignMutationResult{}, fmt.Errorf("invalid campaign mutation result")
	}
	return response.Results[0], nil
}

func safeCampaignMutation(response providerResponse, call sdkgo.Call, operation string, success, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[CampaignMutationResult] {
	if errors.Is(response.err, errCredential) {
		return campaignMutationBranch(defect, operation, sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return campaignMutationBranch(invalid, operation, sdkgo.FailureResponseTooLarge, "Google Ads response exceeded the limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[CampaignMutationResult](adsFailure(operation, sdkgo.FailureTransport, "Google Ads request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[CampaignMutationResult](adsFailure(operation, kind, "Google Ads asked to retry"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return campaignMutationBranch(rejected, operation, sdkgo.FailureProviderRejection, "Google Ads rejected HTTP "+strconv.Itoa(response.status))
	}
	result, err := decodeCampaignMutation(response.body)
	if err != nil {
		return campaignMutationBranch(invalid, operation, sdkgo.FailureProtocol, "Google Ads returned invalid campaign JSON")
	}
	return sdkgo.NewMutationBranch(success, result, nil, campaignReceipt(call, result.ResourceName))
}

func campaignMutationBranch(branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CampaignMutationResult] {
	failure := adsFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, CampaignMutationResult{}, &failure, sdkgo.Receipt{})
}

func uncertainCampaign(call sdkgo.Call, message string) sdkgo.MutationAttempt[CampaignMutationResult] {
	return sdkgo.NewMutationUncertain(CampaignMutationResult{}, adsFailure("createCampaign", sdkgo.FailureTransport, message), campaignReceipt(call, ""))
}

func adsFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "google-ads", Operation: operation, Message: message}
}

func campaignReceipt(call sdkgo.Call, resourceName string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "google-ads", ProviderObjectID: resourceName}
}
