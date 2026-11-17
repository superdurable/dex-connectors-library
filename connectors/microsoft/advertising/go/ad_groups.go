// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const adGroupsPath = "/CampaignManagement/v13/AdGroups"

// AdGroupSummary contains the ad group fields used by Flows.
type AdGroupSummary struct {
	// ID is the provider's numeric ad group identifier.
	ID string `json:"id"`
	// Name is the display name within the campaign.
	Name string `json:"name"`
	// Status is Active or Paused.
	Status string `json:"status"`
}

// AdGroupsPage contains the ad groups in one campaign.
type AdGroupsPage struct {
	// AdGroups are the returned identities and statuses.
	AdGroups []AdGroupSummary `json:"adGroups"`
}

// ListAdGroupsInput identifies one campaign.
type ListAdGroupsInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// CampaignID is the owning campaign's numeric ID.
	CampaignID string `json:"campaignId"`
}

// GetAdGroupInput identifies one ad group in a campaign.
type GetAdGroupInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// CampaignID is the owning campaign's numeric ID.
	CampaignID string `json:"campaignId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
}

// CreateAdGroupInput names one paused ad group.
type CreateAdGroupInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// CampaignID is the owning campaign's numeric ID.
	CampaignID string `json:"campaignId"`
	// Name is unique within the campaign and at most 256 characters.
	Name string `json:"name"`
}

// UpdateAdGroupInput replaces the supplied ad group fields.
type UpdateAdGroupInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// CampaignID is the owning campaign's numeric ID.
	CampaignID string `json:"campaignId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// Name replaces the name when nonblank.
	Name string `json:"name,omitempty"`
	// Status replaces the status when Active or Paused.
	Status string `json:"status,omitempty"`
}

// DeleteAdGroupInput identifies an ad group to remove.
type DeleteAdGroupInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// CampaignID is the owning campaign's numeric ID.
	CampaignID string `json:"campaignId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
}

// AdGroupMutationResult identifies the affected ad group.
type AdGroupMutationResult struct {
	// AdGroupID is the provider's numeric ID.
	AdGroupID string `json:"adGroupId"`
}

// ListAdGroupsOperation reads one campaign's ad groups.
type ListAdGroupsOperation struct{ client *Client }

// GetAdGroupOperation reads one ad group.
type GetAdGroupOperation struct{ client *Client }

// CreateAdGroupOperation adds a paused ad group.
type CreateAdGroupOperation struct{ client *Client }

// UpdateAdGroupOperation changes an ad group's name or status.
type UpdateAdGroupOperation struct{ client *Client }

// DeleteAdGroupOperation removes an ad group.
type DeleteAdGroupOperation struct{ client *Client }

// ListAdGroups binds the Query to this client.
func (client *Client) ListAdGroups() ListAdGroupsOperation { return ListAdGroupsOperation{client} }

// GetAdGroup binds the Query to this client.
func (client *Client) GetAdGroup() GetAdGroupOperation { return GetAdGroupOperation{client} }

// CreateAdGroup binds the Mutation to this client.
func (client *Client) CreateAdGroup() CreateAdGroupOperation { return CreateAdGroupOperation{client} }

// UpdateAdGroup binds the Mutation to this client.
func (client *Client) UpdateAdGroup() UpdateAdGroupOperation { return UpdateAdGroupOperation{client} }

// DeleteAdGroup binds the Mutation to this client.
func (client *Client) DeleteAdGroup() DeleteAdGroupOperation { return DeleteAdGroupOperation{client} }

// Definition returns the generated ad group list Query.
func (ListAdGroupsOperation) Definition() sdkgo.QueryDefinition { return ListAdGroupsDefinition }

// Definition returns the generated ad group lookup Query.
func (GetAdGroupOperation) Definition() sdkgo.QueryDefinition { return GetAdGroupDefinition }

// Definition returns the generated ad group create Mutation.
func (CreateAdGroupOperation) Definition() sdkgo.MutationDefinition { return CreateAdGroupDefinition }

// Definition returns the generated ad group update Mutation.
func (UpdateAdGroupOperation) Definition() sdkgo.MutationDefinition { return UpdateAdGroupDefinition }

// Definition returns the generated ad group delete Mutation.
func (DeleteAdGroupOperation) Definition() sdkgo.MutationDefinition { return DeleteAdGroupDefinition }

// IdempotencyKey identifies the bounded create attempt.
func (CreateAdGroupOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAdGroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies an absolute ad group replacement.
func (UpdateAdGroupOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAdGroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies an ad group deletion.
func (DeleteAdGroupOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAdGroupInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists all ad groups returned for one campaign.
func (operation ListAdGroupsOperation) Invoke(call sdkgo.Call, input ListAdGroupsInput) sdkgo.QueryAttempt[AdGroupsPage] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.CampaignID) {
		return adGroupQueryBranch(ListAdGroupsBranchDefect, AdGroupsPage{}, "listAdGroups", sdkgo.FailureValidation, "account or campaign ID is invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodPost, adGroupsPath+"/QueryByCampaignId", input.AccountID, map[string]any{"CampaignId": input.CampaignID})
	if response.credentialFailure {
		return adGroupQueryBranch(ListAdGroupsBranchDefect, AdGroupsPage{}, "listAdGroups", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewQueryRetry[AdGroupsPage](campaignFailure("listAdGroups", response.failureKind(), "ad group list is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return adGroupQueryBranch(ListAdGroupsBranchProviderRejected, AdGroupsPage{}, "listAdGroups", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeAdGroups(response.body)
	if response.err != nil || err != nil {
		return adGroupQueryBranch(ListAdGroupsBranchInvalidResponse, AdGroupsPage{}, "listAdGroups", sdkgo.FailureProtocol, "Microsoft Advertising returned invalid ad groups")
	}
	return sdkgo.NewQueryBranch(ListAdGroupsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one ad group by ID.
func (operation GetAdGroupOperation) Invoke(call sdkgo.Call, input GetAdGroupInput) sdkgo.QueryAttempt[AdGroupSummary] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.CampaignID) || !validAdvertisingID(input.AdGroupID) {
		return adGroupQueryBranch(GetAdGroupBranchDefect, AdGroupSummary{}, "getAdGroup", sdkgo.FailureValidation, "ad group IDs are invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodPost, adGroupsPath+"/QueryByIds", input.AccountID, map[string]any{"CampaignId": input.CampaignID, "AdGroupIds": []string{input.AdGroupID}})
	if response.credentialFailure {
		return adGroupQueryBranch(GetAdGroupBranchDefect, AdGroupSummary{}, "getAdGroup", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewQueryRetry[AdGroupSummary](campaignFailure("getAdGroup", response.failureKind(), "ad group lookup is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return adGroupQueryBranch(GetAdGroupBranchProviderRejected, AdGroupSummary{}, "getAdGroup", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	if isSingleNullEntity(response.body, "AdGroups") {
		return sdkgo.NewQueryBranch(GetAdGroupBranchNotFound, AdGroupSummary{}, nil, sdkgo.Receipt{})
	}
	page, err := decodeAdGroups(response.body)
	if response.err != nil || err != nil || len(page.AdGroups) > 1 {
		return adGroupQueryBranch(GetAdGroupBranchInvalidResponse, AdGroupSummary{}, "getAdGroup", sdkgo.FailureProtocol, "Microsoft Advertising returned invalid ad group JSON")
	}
	if len(page.AdGroups) == 0 {
		return sdkgo.NewQueryBranch(GetAdGroupBranchNotFound, AdGroupSummary{}, nil, sdkgo.Receipt{})
	}
	if page.AdGroups[0].ID != input.AdGroupID {
		return adGroupQueryBranch(GetAdGroupBranchInvalidResponse, AdGroupSummary{}, "getAdGroup", sdkgo.FailureProtocol, "Microsoft Advertising returned a different ad group")
	}
	return sdkgo.NewQueryBranch(GetAdGroupBranchFound, page.AdGroups[0], nil, sdkgo.Receipt{})
}

// Invoke creates a paused ad group with a bounded send cap and name read-back.
func (operation CreateAdGroupOperation) Invoke(call sdkgo.Call, input CreateAdGroupInput) sdkgo.MutationAttempt[AdGroupMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.CampaignID) || strings.TrimSpace(input.Name) != input.Name || input.Name == "" || len(input.Name) > 256 {
		return adGroupWriteBranch(CreateAdGroupBranchDefect, AdGroupMutationResult{}, "createAdGroup", sdkgo.FailureValidation, "ad group create input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateAdGroupDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return adGroupWriteBranch(CreateAdGroupBranchDefect, AdGroupMutationResult{}, "createAdGroup", sdkgo.FailureLocalDefect, "ad group dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return adGroupWriteBranch(CreateAdGroupBranchDefect, AdGroupMutationResult{}, "createAdGroup", sdkgo.FailureLocalDefect, "ad group dispatch checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		readBack := operation.client.requestCampaign(call, http.MethodPost, adGroupsPath+"/QueryByCampaignId", input.AccountID, map[string]any{"CampaignId": input.CampaignID})
		if readBack.status == http.StatusOK && readBack.err == nil {
			page, decodeErr := decodeAdGroups(readBack.body)
			if decodeErr == nil {
				for _, group := range page.AdGroups {
					if strings.EqualFold(group.Name, input.Name) {
						return sdkgo.NewMutationBranch(CreateAdGroupBranchCreated, AdGroupMutationResult{AdGroupID: group.ID}, nil, sdkgo.Receipt{})
					}
				}
			}
		}
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(AdGroupMutationResult{}, campaignFailure("createAdGroup", sdkgo.FailureAvailability, "ad group create outcome cannot be confirmed"), sdkgo.Receipt{})
	}
	var response campaignHTTPResponse
	err = fence.Dispatch(func(dispatchContext context.Context) error {
		response = operation.client.requestCampaignWithContext(dispatchContext, call, http.MethodPost, adGroupsPath, input.AccountID, map[string]any{"CampaignId": input.CampaignID, "AdGroups": []any{map[string]any{"Name": input.Name, "Status": "Paused"}}})
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[AdGroupMutationResult](campaignFailure("createAdGroup", sdkgo.FailureTransport, "ad group dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AdGroupMutationResult](campaignFailure("createAdGroup", sdkgo.FailureTransport, "ad group retry checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[AdGroupMutationResult](campaignFailure("createAdGroup", response.failureKind(), "Microsoft Advertising temporarily refused the create"), response.retryAfter)
	}
	if response.credentialFailure {
		return adGroupWriteBranch(CreateAdGroupBranchDefect, AdGroupMutationResult{}, "createAdGroup", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[AdGroupMutationResult](campaignFailure("createAdGroup", sdkgo.FailureAvailability, "ad group create outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return adGroupWriteBranch(CreateAdGroupBranchProviderRejected, AdGroupMutationResult{}, "createAdGroup", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	var result struct {
		AdGroupIDs    []string          `json:"AdGroupIds"`
		PartialErrors []json.RawMessage `json:"PartialErrors"`
	}
	if json.Unmarshal(response.body, &result) != nil || len(result.AdGroupIDs) != 1 || !validAdvertisingID(result.AdGroupIDs[0]) || len(result.PartialErrors) != 0 {
		return sdkgo.NewMutationUncertain(AdGroupMutationResult{}, campaignFailure("createAdGroup", sdkgo.FailureProtocol, "ad group create response is incomplete"), sdkgo.Receipt{})
	}
	if err := fence.RecordProviderID(result.AdGroupIDs[0]); err != nil {
		return sdkgo.NewMutationUncertain(AdGroupMutationResult{}, campaignFailure("createAdGroup", sdkgo.FailureLocalDefect, "ad group checkpoint is unavailable"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateAdGroupBranchCreated, AdGroupMutationResult{AdGroupID: result.AdGroupIDs[0]}, nil, sdkgo.Receipt{})
}

// Invoke replaces the supplied name or status on one ad group.
func (operation UpdateAdGroupOperation) Invoke(call sdkgo.Call, input UpdateAdGroupInput) sdkgo.MutationAttempt[AdGroupMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.CampaignID) || !validAdvertisingID(input.AdGroupID) || len(input.Name) > 256 || (input.Name == "" && input.Status == "") || (input.Status != "" && input.Status != "Active" && input.Status != "Paused") {
		return adGroupWriteBranch(UpdateAdGroupBranchDefect, AdGroupMutationResult{}, "updateAdGroup", sdkgo.FailureValidation, "ad group update is invalid")
	}
	group := map[string]any{"Id": input.AdGroupID}
	if input.Name != "" {
		group["Name"] = input.Name
	}
	if input.Status != "" {
		group["Status"] = input.Status
	}
	response := operation.client.requestCampaign(call, http.MethodPut, adGroupsPath, input.AccountID, map[string]any{"CampaignId": input.CampaignID, "AdGroups": []any{group}})
	return adGroupSafeWrite("updateAdGroup", UpdateAdGroupBranchUpdated, UpdateAdGroupBranchProviderRejected, UpdateAdGroupBranchInvalidResponse, UpdateAdGroupBranchDefect, input.AdGroupID, response)
}

// Invoke deletes one ad group by ID.
func (operation DeleteAdGroupOperation) Invoke(call sdkgo.Call, input DeleteAdGroupInput) sdkgo.MutationAttempt[AdGroupMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.CampaignID) || !validAdvertisingID(input.AdGroupID) {
		return adGroupWriteBranch(DeleteAdGroupBranchDefect, AdGroupMutationResult{}, "deleteAdGroup", sdkgo.FailureValidation, "ad group IDs are invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodDelete, adGroupsPath, input.AccountID, map[string]any{"CampaignId": input.CampaignID, "AdGroupIds": []string{input.AdGroupID}})
	return adGroupSafeWrite("deleteAdGroup", DeleteAdGroupBranchDeleted, DeleteAdGroupBranchProviderRejected, DeleteAdGroupBranchInvalidResponse, DeleteAdGroupBranchDefect, input.AdGroupID, response)
}

func decodeAdGroups(body []byte) (AdGroupsPage, error) {
	var wire struct {
		AdGroups []struct {
			ID     string `json:"Id"`
			Name   string `json:"Name"`
			Status string `json:"Status"`
		} `json:"AdGroups"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.AdGroups == nil {
		return AdGroupsPage{}, fmt.Errorf("ad groups are missing or malformed")
	}
	page := AdGroupsPage{AdGroups: make([]AdGroupSummary, 0, len(wire.AdGroups))}
	for _, group := range wire.AdGroups {
		if !validAdvertisingID(group.ID) || group.Name == "" || (group.Status != "Active" && group.Status != "Paused" && group.Status != "Expired") {
			return AdGroupsPage{}, fmt.Errorf("ad group identity is invalid")
		}
		page.AdGroups = append(page.AdGroups, AdGroupSummary{ID: group.ID, Name: group.Name, Status: group.Status})
	}
	return page, nil
}

func adGroupQueryBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := campaignFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func adGroupWriteBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := campaignFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func adGroupSafeWrite(operation string, success, rejection, invalid, defect sdkgo.BranchID, adGroupID string, response campaignHTTPResponse) sdkgo.MutationAttempt[AdGroupMutationResult] {
	if response.credentialFailure {
		return adGroupWriteBranch(defect, AdGroupMutationResult{}, operation, sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewMutationRetry[AdGroupMutationResult](campaignFailure(operation, response.failureKind(), "ad group write is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return adGroupWriteBranch(rejection, AdGroupMutationResult{}, operation, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return adGroupWriteBranch(invalid, AdGroupMutationResult{}, operation, sdkgo.FailureProtocol, "ad group response is unreadable")
	}
	if response.status == http.StatusOK && len(response.body) > 0 {
		var wire struct {
			PartialErrors []json.RawMessage `json:"PartialErrors"`
		}
		if json.Unmarshal(response.body, &wire) != nil {
			return adGroupWriteBranch(invalid, AdGroupMutationResult{}, operation, sdkgo.FailureProtocol, "ad group response is invalid")
		}
		if len(wire.PartialErrors) != 0 {
			return adGroupWriteBranch(rejection, AdGroupMutationResult{}, operation, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected the ad group")
		}
	}
	return sdkgo.NewMutationBranch(success, AdGroupMutationResult{AdGroupID: adGroupID}, nil, sdkgo.Receipt{})
}
