// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const campaignsPath = "/CampaignManagement/v13/Campaigns"

// GetCampaignInput identifies one campaign in an advertising account.
type GetCampaignInput struct {
	// AccountID is the numeric aid in the signed-in Campaigns URL.
	AccountID string `json:"accountId"`
	// CampaignID is the numeric campaign ID shown in Microsoft Advertising.
	CampaignID string `json:"campaignId"`
}

// CreateCampaignInput supplies the required settings for a paused search campaign.
type CreateCampaignInput struct {
	// AccountID is the numeric aid in the signed-in Campaigns URL.
	AccountID string `json:"accountId"`
	// Name is unique among active and paused campaigns and has at most 128 characters.
	Name string `json:"name"`
	// DailyBudget is the positive amount in the account's billing currency.
	DailyBudget float64 `json:"dailyBudget"`
	// TimeZone is a Microsoft Advertising time-zone value for the account's market.
	TimeZone string `json:"timeZone"`
}

// UpdateCampaignInput sets specified mutable campaign fields.
type UpdateCampaignInput struct {
	// AccountID is the numeric aid in the signed-in Campaigns URL.
	AccountID string `json:"accountId"`
	// CampaignID is the numeric campaign ID shown in Microsoft Advertising.
	CampaignID string `json:"campaignId"`
	// Name replaces the campaign name when nonempty.
	Name string `json:"name,omitempty"`
	// Status replaces the status when Active or Paused; blank leaves it unchanged.
	Status string `json:"status,omitempty"`
	// DailyBudget replaces the positive amount in account currency when set.
	DailyBudget *float64 `json:"dailyBudget,omitempty"`
}

// DeleteCampaignInput identifies the campaign to delete.
type DeleteCampaignInput struct {
	// AccountID is the numeric aid in the signed-in Campaigns URL.
	AccountID string `json:"accountId"`
	// CampaignID is the numeric campaign ID shown in Microsoft Advertising.
	CampaignID string `json:"campaignId"`
}

// CampaignMutationResult identifies the campaign affected by a write.
type CampaignMutationResult struct {
	// CampaignID is the numeric Microsoft Advertising campaign ID.
	CampaignID string `json:"campaignId"`
}

// GetCampaignOperation reads one campaign.
type GetCampaignOperation struct{ client *Client }

// GetCampaign binds the Query to this client.
func (client *Client) GetCampaign() GetCampaignOperation { return GetCampaignOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetCampaignOperation) Definition() sdkgo.QueryDefinition { return GetCampaignDefinition }

// Invoke reads one campaign and classifies provider responses.
func (operation GetCampaignOperation) Invoke(call sdkgo.Call, input GetCampaignInput) sdkgo.QueryAttempt[CampaignSummary] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.CampaignID) {
		return getCampaignBranch(GetCampaignBranchDefect, CampaignSummary{}, sdkgo.FailureValidation, "account or campaign ID is invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodPost, campaignsPath+"/QueryByIds", input.AccountID, map[string]any{
		"AccountId": input.AccountID, "CampaignIds": []string{input.CampaignID}, "CampaignType": allCampaignTypes,
	})
	if response.credentialFailure {
		return getCampaignBranch(GetCampaignBranchDefect, CampaignSummary{}, sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewQueryRetry[CampaignSummary](campaignFailure("getCampaign", response.failureKind(), "Microsoft Advertising campaign lookup is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return getCampaignBranch(GetCampaignBranchProviderRejected, CampaignSummary{}, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return getCampaignBranch(GetCampaignBranchInvalidResponse, CampaignSummary{}, sdkgo.FailureProtocol, "Microsoft Advertising campaign response is unreadable")
	}
	page, err := decodeCampaigns(response.body)
	if err != nil || len(page.Campaigns) > 1 {
		return getCampaignBranch(GetCampaignBranchInvalidResponse, CampaignSummary{}, sdkgo.FailureProtocol, "Microsoft Advertising returned invalid campaign JSON")
	}
	if len(page.Campaigns) == 0 {
		return getCampaignBranch(GetCampaignBranchNotFound, CampaignSummary{}, sdkgo.FailureNotFound, "Microsoft Advertising did not find the campaign")
	}
	if page.Campaigns[0].ID != input.CampaignID {
		return getCampaignBranch(GetCampaignBranchInvalidResponse, CampaignSummary{}, sdkgo.FailureProtocol, "Microsoft Advertising returned a different campaign")
	}
	return sdkgo.NewQueryBranch(GetCampaignBranchFound, page.Campaigns[0], nil, sdkgo.Receipt{})
}

// CreateCampaignOperation creates one paused search campaign.
type CreateCampaignOperation struct{ client *Client }

// CreateCampaign binds the Mutation to this client.
func (client *Client) CreateCampaign() CreateCampaignOperation {
	return CreateCampaignOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreateCampaignOperation) Definition() sdkgo.MutationDefinition { return CreateCampaignDefinition }

// IdempotencyKey identifies the logical create; Microsoft Advertising does not deduplicate it.
func (CreateCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a campaign with bounded resends after read-back by its unique name.
func (operation CreateCampaignOperation) Invoke(call sdkgo.Call, input CreateCampaignInput) sdkgo.MutationAttempt[CampaignMutationResult] {
	if !validAdvertisingID(input.AccountID) || strings.TrimSpace(input.Name) != input.Name || input.Name == "" || len(input.Name) > 128 ||
		!isPositiveFinite(input.DailyBudget) || input.TimeZone == "" || len(input.TimeZone) > 100 {
		return campaignWriteBranch("createCampaign", CreateCampaignBranchDefect, CampaignMutationResult{}, sdkgo.FailureValidation, "campaign input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateCampaignDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return campaignWriteBranch("createCampaign", CreateCampaignBranchDefect, CampaignMutationResult{}, sdkgo.FailureLocalDefect, "campaign dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return campaignWriteBranch("createCampaign", CreateCampaignBranchDefect, CampaignMutationResult{}, sdkgo.FailureLocalDefect, "campaign dispatch checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		readBack := operation.client.requestCampaign(call, http.MethodPost, listCampaignsPath, input.AccountID, map[string]any{
			"AccountId": input.AccountID, "CampaignType": allCampaignTypes,
		})
		if readBack.status == http.StatusOK && readBack.err == nil {
			page, decodeErr := decodeCampaigns(readBack.body)
			if decodeErr == nil {
				for _, campaign := range page.Campaigns {
					if strings.EqualFold(campaign.Name, input.Name) {
						return sdkgo.NewMutationBranch(CreateCampaignBranchCreated, CampaignMutationResult{CampaignID: campaign.ID}, nil, sdkgo.Receipt{})
					}
				}
			}
		}
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(CampaignMutationResult{}, campaignFailure("createCampaign", sdkgo.FailureAvailability, "campaign create outcome cannot be confirmed"), sdkgo.Receipt{})
	}
	payload := map[string]any{"AccountId": input.AccountID, "Campaigns": []any{map[string]any{
		"Name": input.Name, "CampaignType": "Search", "BudgetType": "DailyBudgetStandard", "DailyBudget": input.DailyBudget,
		"TimeZone": input.TimeZone, "Status": "Paused",
	}}}
	var response campaignHTTPResponse
	err = fence.Dispatch(func(dispatchContext context.Context) error {
		response = operation.client.requestCampaignWithContext(dispatchContext, call, http.MethodPost, campaignsPath, input.AccountID, payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CampaignMutationResult](campaignFailure("createCampaign", sdkgo.FailureTransport, "campaign dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[CampaignMutationResult](campaignFailure("createCampaign", sdkgo.FailureTransport, "campaign retry checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[CampaignMutationResult](campaignFailure("createCampaign", response.failureKind(), "Microsoft Advertising temporarily refused the create"), response.retryAfter)
	}
	if response.credentialFailure {
		return campaignWriteBranch("createCampaign", CreateCampaignBranchDefect, CampaignMutationResult{}, sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CampaignMutationResult](campaignFailure("createCampaign", sdkgo.FailureAvailability, "campaign create outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return campaignWriteBranch("createCampaign", CreateCampaignBranchProviderRejected, CampaignMutationResult{}, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	var result struct {
		CampaignIDs   []string          `json:"CampaignIds"`
		PartialErrors []json.RawMessage `json:"PartialErrors"`
	}
	if json.Unmarshal(response.body, &result) != nil || len(result.CampaignIDs) != 1 || !validAdvertisingID(result.CampaignIDs[0]) || len(result.PartialErrors) != 0 {
		return sdkgo.NewMutationUncertain(CampaignMutationResult{}, campaignFailure("createCampaign", sdkgo.FailureProtocol, "campaign create response is incomplete"), sdkgo.Receipt{})
	}
	if err := fence.RecordProviderID(result.CampaignIDs[0]); err != nil {
		return sdkgo.NewMutationUncertain(CampaignMutationResult{}, campaignFailure("createCampaign", sdkgo.FailureLocalDefect, "campaign ID checkpoint is unavailable"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateCampaignBranchCreated, CampaignMutationResult{CampaignID: result.CampaignIDs[0]}, nil, sdkgo.Receipt{})
}

// UpdateCampaignOperation changes mutable campaign fields.
type UpdateCampaignOperation struct{ client *Client }

// UpdateCampaign binds the Mutation to this client.
func (client *Client) UpdateCampaign() UpdateCampaignOperation {
	return UpdateCampaignOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (UpdateCampaignOperation) Definition() sdkgo.MutationDefinition { return UpdateCampaignDefinition }

// IdempotencyKey identifies the logical absolute update.
func (UpdateCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends an absolute update for each supplied field.
func (operation UpdateCampaignOperation) Invoke(call sdkgo.Call, input UpdateCampaignInput) sdkgo.MutationAttempt[CampaignMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.CampaignID) || len(input.Name) > 128 ||
		(input.Status != "" && input.Status != "Active" && input.Status != "Paused") ||
		(input.DailyBudget != nil && !isPositiveFinite(*input.DailyBudget)) ||
		(input.Name == "" && input.Status == "" && input.DailyBudget == nil) {
		return campaignWriteBranch("updateCampaign", UpdateCampaignBranchDefect, CampaignMutationResult{}, sdkgo.FailureValidation, "campaign update is invalid")
	}
	campaign := map[string]any{"Id": input.CampaignID}
	if input.Name != "" {
		campaign["Name"] = input.Name
	}
	if input.Status != "" {
		campaign["Status"] = input.Status
	}
	if input.DailyBudget != nil {
		campaign["DailyBudget"] = *input.DailyBudget
	}
	response := operation.client.requestCampaign(call, http.MethodPut, campaignsPath, input.AccountID, map[string]any{
		"AccountId": input.AccountID, "Campaigns": []any{campaign},
	})
	return campaignSafeWriteResult("updateCampaign", UpdateCampaignBranchUpdated, UpdateCampaignBranchProviderRejected, UpdateCampaignBranchInvalidResponse, UpdateCampaignBranchDefect, input.CampaignID, response)
}

// DeleteCampaignOperation removes one campaign.
type DeleteCampaignOperation struct{ client *Client }

// DeleteCampaign binds the Mutation to this client.
func (client *Client) DeleteCampaign() DeleteCampaignOperation {
	return DeleteCampaignOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (DeleteCampaignOperation) Definition() sdkgo.MutationDefinition { return DeleteCampaignDefinition }

// IdempotencyKey identifies the logical deletion.
func (DeleteCampaignOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCampaignInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one campaign, accepting empty success responses.
func (operation DeleteCampaignOperation) Invoke(call sdkgo.Call, input DeleteCampaignInput) sdkgo.MutationAttempt[CampaignMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.CampaignID) {
		return campaignWriteBranch("deleteCampaign", DeleteCampaignBranchDefect, CampaignMutationResult{}, sdkgo.FailureValidation, "campaign IDs are invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodDelete, campaignsPath, input.AccountID, map[string]any{
		"AccountId": input.AccountID, "CampaignIds": []string{input.CampaignID},
	})
	return campaignSafeWriteResult("deleteCampaign", DeleteCampaignBranchDeleted, DeleteCampaignBranchProviderRejected, DeleteCampaignBranchInvalidResponse, DeleteCampaignBranchDefect, input.CampaignID, response)
}

type campaignHTTPResponse struct {
	status            int
	body              []byte
	retryAfter        time.Duration
	err               error
	credentialFailure bool
}

func (client *Client) requestCampaign(call sdkgo.Call, method, path, accountID string, payload any) campaignHTTPResponse {
	return client.requestCampaignWithContext(call.Context, call, method, path, accountID, payload)
}

func (client *Client) requestCampaignWithContext(ctx context.Context, call sdkgo.Call, method, path, accountID string, payload any) campaignHTTPResponse {
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, call, client.refreshDriver)
	if err != nil {
		return campaignHTTPResponse{err: err, credentialFailure: sdkgo.IsReauthorizationRequired(err)}
	}
	accessToken := credentials.AccessToken.Reveal()
	developerToken := credentials.DeveloperToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) || !providerhttp.IsHeaderSafeCredential(developerToken) {
		return campaignHTTPResponse{credentialFailure: true}
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return campaignHTTPResponse{err: err}
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, bytes.NewReader(requestBody))
	if err != nil {
		return campaignHTTPResponse{err: err}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("DeveloperToken", developerToken)
	request.Header.Set("CustomerId", client.customerID)
	request.Header.Set("CustomerAccountId", accountID)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return campaignHTTPResponse{err: err}
	}
	defer func() { _ = response.Body.Close() }()
	body, readErr := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) || bytes.Contains(body, []byte(developerToken)) {
		return campaignHTTPResponse{status: response.StatusCode, credentialFailure: true}
	}
	return campaignHTTPResponse{status: response.StatusCode, body: body, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: readErr}
}

func (response campaignHTTPResponse) shouldRetry() bool {
	return !response.credentialFailure && (response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500)
}

func (response campaignHTTPResponse) failureKind() sdkgo.FailureKind {
	if response.status == http.StatusTooManyRequests {
		return sdkgo.FailureRateLimit
	}
	if response.status >= 500 {
		return sdkgo.FailureAvailability
	}
	return sdkgo.FailureTransport
}

func validAdvertisingID(value string) bool {
	if !advertisingIDPattern.MatchString(value) {
		return false
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

func isPositiveFinite(value float64) bool {
	return value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

func campaignFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "microsoft-advertising", Operation: operation, Message: message}
}

func getCampaignBranch(branch sdkgo.BranchID, value CampaignSummary, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CampaignSummary] {
	failure := campaignFailure("getCampaign", kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func campaignWriteBranch(operation string, branch sdkgo.BranchID, value CampaignMutationResult, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CampaignMutationResult] {
	failure := campaignFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func campaignSafeWriteResult(operation string, success, rejection, invalid, defect sdkgo.BranchID, campaignID string, response campaignHTTPResponse) sdkgo.MutationAttempt[CampaignMutationResult] {
	if response.credentialFailure {
		return campaignWriteBranch(operation, defect, CampaignMutationResult{}, sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewMutationRetry[CampaignMutationResult](campaignFailure(operation, response.failureKind(), "Microsoft Advertising campaign write is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return campaignWriteBranch(operation, rejection, CampaignMutationResult{}, sdkgo.FailureProviderRejection, fmt.Sprintf("Microsoft Advertising rejected HTTP %d", response.status))
	}
	if response.err != nil {
		return campaignWriteBranch(operation, invalid, CampaignMutationResult{}, sdkgo.FailureProtocol, "Microsoft Advertising campaign response is unreadable")
	}
	if response.status == http.StatusOK && len(response.body) > 0 {
		var result struct {
			PartialErrors []json.RawMessage `json:"PartialErrors"`
		}
		if json.Unmarshal(response.body, &result) != nil {
			return campaignWriteBranch(operation, invalid, CampaignMutationResult{}, sdkgo.FailureProtocol, "Microsoft Advertising campaign response is invalid")
		}
		if len(result.PartialErrors) != 0 {
			return campaignWriteBranch(operation, rejection, CampaignMutationResult{}, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected the campaign item")
		}
	}
	return sdkgo.NewMutationBranch(success, CampaignMutationResult{CampaignID: campaignID}, nil, sdkgo.Receipt{})
}
