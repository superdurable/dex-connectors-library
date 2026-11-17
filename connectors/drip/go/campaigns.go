// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drip

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListCampaignsInput selects a page and optional status or sort order.
type ListCampaignsInput struct {
	// AccountID is a numeric ID returned by ListAccounts.
	AccountID string `json:"accountId"`
	// Page is one-based; zero selects the first page.
	Page int `json:"page,omitempty"`
	// Status is draft, active, paused, or blank for all.
	Status string `json:"status,omitempty"`
	// Sort is created_at or name; blank uses Drip's default.
	Sort string `json:"sort,omitempty"`
	// Direction is asc or desc; blank uses Drip's default.
	Direction string `json:"direction,omitempty"`
}

// CampaignIdentityInput identifies one email series campaign.
type CampaignIdentityInput struct {
	// AccountID is a numeric ID returned by ListAccounts.
	AccountID string `json:"accountId"`
	// CampaignID is the numeric ID returned by ListCampaigns.
	CampaignID string `json:"campaignId"`
}

// CampaignMetadata contains a campaign's stable identity and lifecycle state.
type CampaignMetadata struct {
	// ID is Drip's campaign ID.
	ID string `json:"id"`
	// Name is the campaign's private display name.
	Name string `json:"name"`
	// Status is draft, active, or paused.
	Status string `json:"status"`
}

// CampaignsPage is one page of email series campaigns.
type CampaignsPage struct {
	// Campaigns contains the page's metadata.
	Campaigns []CampaignMetadata `json:"campaigns"`
	// Page is the one-based returned page number.
	Page int `json:"page"`
	// TotalPages is the available number of pages.
	TotalPages int `json:"totalPages"`
	// TotalCount is the total number of campaigns.
	TotalCount int `json:"totalCount"`
}

// CampaignState confirms an absolute lifecycle state.
type CampaignState struct {
	// ID is the campaign ID.
	ID string `json:"id"`
	// Status is active or paused.
	Status string `json:"status"`
}

// ListCampaignsOperation reads one campaign page.
type ListCampaignsOperation struct{ client *Client }

// GetCampaignOperation reads a campaign.
type GetCampaignOperation struct{ client *Client }

// ActivateCampaignOperation makes a campaign active.
type ActivateCampaignOperation struct{ client *Client }

// PauseCampaignOperation makes a campaign paused.
type PauseCampaignOperation struct{ client *Client }

// ListCampaigns binds the page Query to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// GetCampaign binds the campaign Query to this client.
func (client *Client) GetCampaign() GetCampaignOperation { return GetCampaignOperation{client} }

// ActivateCampaign binds the activation Mutation to this client.
func (client *Client) ActivateCampaign() ActivateCampaignOperation {
	return ActivateCampaignOperation{client}
}

// PauseCampaign binds the pause Mutation to this client.
func (client *Client) PauseCampaign() PauseCampaignOperation { return PauseCampaignOperation{client} }

// Definition returns the generated campaign page contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Definition returns the generated campaign retrieval contract.
func (GetCampaignOperation) Definition() sdkgo.QueryDefinition { return GetCampaignDefinition }

// Definition returns the generated activation contract.
func (ActivateCampaignOperation) Definition() sdkgo.MutationDefinition {
	return ActivateCampaignDefinition
}

// Definition returns the generated pause contract.
func (PauseCampaignOperation) Definition() sdkgo.MutationDefinition { return PauseCampaignDefinition }

// IdempotencyKey records the call identity for a state-converging action.
func (ActivateCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ CampaignIdentityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the call identity for a state-converging action.
func (PauseCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ CampaignIdentityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists one bounded campaign page.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListCampaignsInput) sdkgo.QueryAttempt[CampaignsPage] {
	branches := queryBranches{found: ListCampaignsBranchListed, rejected: ListCampaignsBranchProviderRejected, invalid: ListCampaignsBranchInvalidResponse, defect: ListCampaignsBranchDefect}
	if !validAccountID(input.AccountID) || input.Page < 0 || input.Status != "" && input.Status != "draft" && input.Status != "active" && input.Status != "paused" || input.Sort != "" && input.Sort != "created_at" && input.Sort != "name" || input.Direction != "" && input.Direction != "asc" && input.Direction != "desc" {
		return queryBranch("listCampaigns", branches.defect, CampaignsPage{}, sdkgo.FailureValidation, "account ID or campaign list options are invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	if input.Sort != "" {
		query.Set("sort", input.Sort)
	}
	if input.Direction != "" {
		query.Set("direction", input.Direction)
	}
	return invokeQuery(call, operation.client, "listCampaigns", "/"+input.AccountID+"/campaigns?"+query.Encode(), branches, decodeCampaignsPage)
}

// Invoke reads one campaign by stable ID.
func (operation GetCampaignOperation) Invoke(call sdkgo.Call, input CampaignIdentityInput) sdkgo.QueryAttempt[CampaignMetadata] {
	branches := queryBranches{found: GetCampaignBranchFound, notFound: GetCampaignBranchNotFound, rejected: GetCampaignBranchProviderRejected, invalid: GetCampaignBranchInvalidResponse, defect: GetCampaignBranchDefect}
	if !validCampaignIdentity(input) {
		return queryBranch("getCampaign", branches.defect, CampaignMetadata{}, sdkgo.FailureValidation, "account or campaign ID is invalid")
	}
	return invokeQuery(call, operation.client, "getCampaign", campaignPath(input), branches, decodeCampaign)
}

// Invoke ensures the campaign is active.
func (operation ActivateCampaignOperation) Invoke(call sdkgo.Call, input CampaignIdentityInput) sdkgo.MutationAttempt[CampaignState] {
	return invokeCampaignState(call, operation.client, "activateCampaign", input, "activate", "active", ActivateCampaignBranchActive, ActivateCampaignBranchProviderRejected, ActivateCampaignBranchDefect)
}

// Invoke ensures the campaign is paused.
func (operation PauseCampaignOperation) Invoke(call sdkgo.Call, input CampaignIdentityInput) sdkgo.MutationAttempt[CampaignState] {
	return invokeCampaignState(call, operation.client, "pauseCampaign", input, "pause", "paused", PauseCampaignBranchPaused, PauseCampaignBranchProviderRejected, PauseCampaignBranchDefect)
}

func invokeCampaignState(call sdkgo.Call, client *Client, operation string, input CampaignIdentityInput, action, target string, success, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[CampaignState] {
	if !validCampaignIdentity(input) {
		return campaignStateFailure(operation, defect, sdkgo.FailureValidation, "account or campaign ID is invalid")
	}
	_, status, retryAfter, err := client.send(call, http.MethodPost, campaignPath(input)+"/"+action, nil)
	if errors.Is(err, errCredential) {
		return campaignStateFailure(operation, defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return campaignStateRetry(operation, sdkgo.FailureTransport, "Drip campaign request failed", 0)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable || status == http.StatusRequestTimeout || status >= 500 {
		kind := sdkgo.FailureAvailability
		if status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return campaignStateRetry(operation, kind, "Drip campaign action is temporarily unavailable", retryAfter)
	}
	if err != nil {
		return campaignStateRetry(operation, sdkgo.FailureTransport, "Drip campaign response was interrupted", 0)
	}
	if status == http.StatusNoContent {
		return sdkgo.NewMutationBranch(success, CampaignState{ID: input.CampaignID, Status: target}, nil, sdkgo.Receipt{})
	}
	if status == http.StatusUnprocessableEntity {
		metadata, readStatus, readRetryAfter, readErr := client.get(call, campaignPath(input))
		if readErr != nil || readStatus == http.StatusTooManyRequests || readStatus >= 500 {
			return campaignStateRetry(operation, sdkgo.FailureAvailability, "campaign state read-back is unavailable", readRetryAfter)
		}
		if readStatus == http.StatusOK {
			campaign, decodeErr := decodeCampaign(metadata)
			if decodeErr == nil && campaign.ID == input.CampaignID && campaign.Status == target {
				return sdkgo.NewMutationBranch(success, CampaignState{ID: campaign.ID, Status: target}, nil, sdkgo.Receipt{})
			}
		}
	}
	return campaignStateFailure(operation, rejected, sdkgo.FailureProviderRejection, "Drip rejected campaign action with HTTP "+strconv.Itoa(status))
}

func decodeCampaignsPage(body []byte) (CampaignsPage, error) {
	var wire struct {
		Meta struct {
			Page       int `json:"page"`
			TotalPages int `json:"total_pages"`
			TotalCount int `json:"total_count"`
		} `json:"meta"`
		Campaigns []CampaignMetadata `json:"campaigns"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CampaignsPage{}, err
	}
	if wire.Campaigns == nil || wire.Meta.Page < 1 || wire.Meta.TotalPages < 0 || wire.Meta.TotalCount < 0 {
		return CampaignsPage{}, fmt.Errorf("campaign page is incomplete")
	}
	for _, campaign := range wire.Campaigns {
		if !validCampaignMetadata(campaign) {
			return CampaignsPage{}, fmt.Errorf("campaign identity is incomplete")
		}
	}
	return CampaignsPage{Campaigns: wire.Campaigns, Page: wire.Meta.Page, TotalPages: wire.Meta.TotalPages, TotalCount: wire.Meta.TotalCount}, nil
}

func decodeCampaign(body []byte) (CampaignMetadata, error) {
	var wire struct {
		Campaigns []CampaignMetadata `json:"campaigns"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CampaignMetadata{}, err
	}
	if len(wire.Campaigns) != 1 || !validCampaignMetadata(wire.Campaigns[0]) {
		return CampaignMetadata{}, fmt.Errorf("campaign identity is incomplete")
	}
	return wire.Campaigns[0], nil
}

func validCampaignIdentity(input CampaignIdentityInput) bool {
	return validAccountID(input.AccountID) && validAccountID(input.CampaignID)
}
func validCampaignMetadata(campaign CampaignMetadata) bool {
	return validAccountID(campaign.ID) && campaign.Name != "" && (campaign.Status == "draft" || campaign.Status == "active" || campaign.Status == "paused")
}
func campaignPath(input CampaignIdentityInput) string {
	return "/" + input.AccountID + "/campaigns/" + input.CampaignID
}

func campaignStateFailure(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CampaignState] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, CampaignState{}, &failure, sdkgo.Receipt{})
}

func campaignStateRetry(operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CampaignState] {
	return sdkgo.NewMutationRetry[CampaignState](queryFailure(operation, kind, message), retryAfter)
}
