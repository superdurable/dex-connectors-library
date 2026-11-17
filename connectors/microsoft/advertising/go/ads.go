// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const adsPath = "/CampaignManagement/v13/Ads"

// AdSummary identifies one responsive search ad.
type AdSummary struct {
	// ID is the numeric provider ad ID.
	ID string `json:"id"`
	// Status is the provider serving status.
	Status string `json:"status"`
	// FinalURL is the first landing page URL.
	FinalURL string `json:"finalUrl"`
}

// AdsPage contains the responsive search ads in one ad group.
type AdsPage struct {
	// Ads are the ads returned by the provider.
	Ads []AdSummary `json:"ads"`
}

// ListAdsInput identifies the owning ad group.
type ListAdsInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
}

// GetAdInput identifies one ad in an ad group.
type GetAdInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// AdID is the numeric provider ad ID.
	AdID string `json:"adId"`
}

// CreateResponsiveSearchAdInput contains the copy for one paused search ad.
type CreateResponsiveSearchAdInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// FinalURL is an HTTP or HTTPS landing page URL.
	FinalURL string `json:"finalUrl"`
	// Headlines contains 3 to 15 distinct headline texts.
	Headlines []string `json:"headlines"`
	// Descriptions contains 2 to 4 distinct description texts.
	Descriptions []string `json:"descriptions"`
}

// UpdateAdInput replaces an ad's serving status.
type UpdateAdInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// AdID is the numeric provider ad ID.
	AdID string `json:"adId"`
	// Status is Active or Paused.
	Status string `json:"status"`
}

// DeleteAdInput identifies one ad to remove.
type DeleteAdInput struct {
	// AccountID is the numeric aid in Microsoft Advertising.
	AccountID string `json:"accountId"`
	// AdGroupID is the numeric ad group ID.
	AdGroupID string `json:"adGroupId"`
	// AdID is the numeric provider ad ID.
	AdID string `json:"adId"`
}

// AdMutationResult identifies the affected ad.
type AdMutationResult struct {
	// AdID is the numeric provider ad ID.
	AdID string `json:"adId"`
}

// ListAdsOperation lists responsive search ads.
type ListAdsOperation struct{ client *Client }

// GetAdOperation reads one responsive search ad.
type GetAdOperation struct{ client *Client }

// CreateResponsiveSearchAdOperation creates a paused ad.
type CreateResponsiveSearchAdOperation struct{ client *Client }

// UpdateAdOperation replaces an ad's serving status.
type UpdateAdOperation struct{ client *Client }

// DeleteAdOperation removes one ad.
type DeleteAdOperation struct{ client *Client }

// ListAds binds the Query to this client.
func (client *Client) ListAds() ListAdsOperation { return ListAdsOperation{client} }

// GetAd binds the Query to this client.
func (client *Client) GetAd() GetAdOperation { return GetAdOperation{client} }

// CreateResponsiveSearchAd binds the Mutation to this client.
func (client *Client) CreateResponsiveSearchAd() CreateResponsiveSearchAdOperation {
	return CreateResponsiveSearchAdOperation{client}
}

// UpdateAd binds the Mutation to this client.
func (client *Client) UpdateAd() UpdateAdOperation { return UpdateAdOperation{client} }

// DeleteAd binds the Mutation to this client.
func (client *Client) DeleteAd() DeleteAdOperation { return DeleteAdOperation{client} }

// Definition returns the generated ad list Query.
func (ListAdsOperation) Definition() sdkgo.QueryDefinition { return ListAdsDefinition }

// Definition returns the generated ad lookup Query.
func (GetAdOperation) Definition() sdkgo.QueryDefinition { return GetAdDefinition }

// Definition returns the generated ad create Mutation.
func (CreateResponsiveSearchAdOperation) Definition() sdkgo.MutationDefinition {
	return CreateResponsiveSearchAdDefinition
}

// Definition returns the generated ad update Mutation.
func (UpdateAdOperation) Definition() sdkgo.MutationDefinition { return UpdateAdDefinition }

// Definition returns the generated ad delete Mutation.
func (DeleteAdOperation) Definition() sdkgo.MutationDefinition { return DeleteAdDefinition }

// IdempotencyKey identifies this bounded create call.
func (CreateResponsiveSearchAdOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateResponsiveSearchAdInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies an absolute status update.
func (UpdateAdOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAdInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies the ad deletion.
func (DeleteAdOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAdInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists responsive search ads within one ad group.
func (operation ListAdsOperation) Invoke(call sdkgo.Call, input ListAdsInput) sdkgo.QueryAttempt[AdsPage] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) {
		return keywordQueryBranch(ListAdsBranchDefect, AdsPage{}, "listAds", sdkgo.FailureValidation, "account or ad group ID is invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodPost, adsPath+"/QueryByAdGroupId", input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "AdTypes": []string{"ResponsiveSearch"}})
	if response.credentialFailure {
		return keywordQueryBranch(ListAdsBranchDefect, AdsPage{}, "listAds", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewQueryRetry[AdsPage](campaignFailure("listAds", response.failureKind(), "ad list is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return keywordQueryBranch(ListAdsBranchProviderRejected, AdsPage{}, "listAds", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeAds(response.body)
	if response.err != nil || err != nil {
		return keywordQueryBranch(ListAdsBranchInvalidResponse, AdsPage{}, "listAds", sdkgo.FailureProtocol, "Microsoft Advertising returned invalid ads")
	}
	return sdkgo.NewQueryBranch(ListAdsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one responsive search ad by ID.
func (operation GetAdOperation) Invoke(call sdkgo.Call, input GetAdInput) sdkgo.QueryAttempt[AdSummary] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) || !validAdvertisingID(input.AdID) {
		return keywordQueryBranch(GetAdBranchDefect, AdSummary{}, "getAd", sdkgo.FailureValidation, "ad IDs are invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodPost, adsPath+"/QueryByIds", input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "AdIds": []string{input.AdID}, "AdTypes": []string{"ResponsiveSearch"}})
	if response.credentialFailure {
		return keywordQueryBranch(GetAdBranchDefect, AdSummary{}, "getAd", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewQueryRetry[AdSummary](campaignFailure("getAd", response.failureKind(), "ad lookup is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return keywordQueryBranch(GetAdBranchProviderRejected, AdSummary{}, "getAd", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeAds(response.body)
	if response.err != nil || err != nil || len(page.Ads) > 1 {
		return keywordQueryBranch(GetAdBranchInvalidResponse, AdSummary{}, "getAd", sdkgo.FailureProtocol, "Microsoft Advertising returned invalid ad JSON")
	}
	if len(page.Ads) == 0 {
		return sdkgo.NewQueryBranch(GetAdBranchNotFound, AdSummary{}, nil, sdkgo.Receipt{})
	}
	if page.Ads[0].ID != input.AdID {
		return keywordQueryBranch(GetAdBranchInvalidResponse, AdSummary{}, "getAd", sdkgo.FailureProtocol, "Microsoft Advertising returned a different ad")
	}
	return sdkgo.NewQueryBranch(GetAdBranchFound, page.Ads[0], nil, sdkgo.Receipt{})
}

// Invoke creates a paused ad with a bounded send count.
func (operation CreateResponsiveSearchAdOperation) Invoke(call sdkgo.Call, input CreateResponsiveSearchAdInput) sdkgo.MutationAttempt[AdMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) || !validAdURL(input.FinalURL) || !validAdCopy(input.Headlines, 3, 15) || !validAdCopy(input.Descriptions, 2, 4) {
		return keywordWriteBranch(CreateResponsiveSearchAdBranchDefect, AdMutationResult{}, "createResponsiveSearchAd", sdkgo.FailureValidation, "ad create input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateResponsiveSearchAdDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return keywordWriteBranch(CreateResponsiveSearchAdBranchDefect, AdMutationResult{}, "createResponsiveSearchAd", sdkgo.FailureLocalDefect, "ad dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return keywordWriteBranch(CreateResponsiveSearchAdBranchDefect, AdMutationResult{}, "createResponsiveSearchAd", sdkgo.FailureLocalDefect, "ad dispatch checkpoint is invalid")
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(AdMutationResult{}, campaignFailure("createResponsiveSearchAd", sdkgo.FailureAvailability, "ad create outcome cannot be confirmed"), sdkgo.Receipt{})
	}
	// The API permits duplicate responsive ads, so identical copy cannot prove this call succeeded.
	ad := map[string]any{"Type": "ResponsiveSearch", "Status": "Paused", "FinalUrls": []string{input.FinalURL}, "Headlines": adAssetLinks(input.Headlines), "Descriptions": adAssetLinks(input.Descriptions)}
	var response campaignHTTPResponse
	err = fence.Dispatch(func(dispatchContext context.Context) error {
		response = operation.client.requestCampaignWithContext(dispatchContext, call, http.MethodPost, adsPath, input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "Ads": []any{ad}})
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[AdMutationResult](campaignFailure("createResponsiveSearchAd", sdkgo.FailureTransport, "ad dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AdMutationResult](campaignFailure("createResponsiveSearchAd", sdkgo.FailureTransport, "ad retry checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[AdMutationResult](campaignFailure("createResponsiveSearchAd", response.failureKind(), "Microsoft Advertising temporarily refused the create"), response.retryAfter)
	}
	if response.credentialFailure {
		return keywordWriteBranch(CreateResponsiveSearchAdBranchDefect, AdMutationResult{}, "createResponsiveSearchAd", sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[AdMutationResult](campaignFailure("createResponsiveSearchAd", sdkgo.FailureAvailability, "ad create outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return keywordWriteBranch(CreateResponsiveSearchAdBranchProviderRejected, AdMutationResult{}, "createResponsiveSearchAd", sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	var wire struct {
		AdIDs         []string          `json:"AdIds"`
		PartialErrors []json.RawMessage `json:"PartialErrors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.AdIDs) != 1 || !validAdvertisingID(wire.AdIDs[0]) || len(wire.PartialErrors) != 0 {
		return sdkgo.NewMutationUncertain(AdMutationResult{}, campaignFailure("createResponsiveSearchAd", sdkgo.FailureProtocol, "ad create response is incomplete"), sdkgo.Receipt{})
	}
	if err := fence.RecordProviderID(wire.AdIDs[0]); err != nil {
		return sdkgo.NewMutationUncertain(AdMutationResult{}, campaignFailure("createResponsiveSearchAd", sdkgo.FailureLocalDefect, "ad checkpoint is unavailable"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateResponsiveSearchAdBranchCreated, AdMutationResult{AdID: wire.AdIDs[0]}, nil, sdkgo.Receipt{})
}

// Invoke replaces an ad's serving status.
func (operation UpdateAdOperation) Invoke(call sdkgo.Call, input UpdateAdInput) sdkgo.MutationAttempt[AdMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) || !validAdvertisingID(input.AdID) || (input.Status != "Active" && input.Status != "Paused") {
		return keywordWriteBranch(UpdateAdBranchDefect, AdMutationResult{}, "updateAd", sdkgo.FailureValidation, "ad update input is invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodPut, adsPath, input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "Ads": []any{map[string]any{"Id": input.AdID, "Type": "ResponsiveSearch", "Status": input.Status}}})
	return adSafeWrite("updateAd", UpdateAdBranchUpdated, UpdateAdBranchProviderRejected, UpdateAdBranchInvalidResponse, UpdateAdBranchDefect, input.AdID, response)
}

// Invoke deletes one ad by ID.
func (operation DeleteAdOperation) Invoke(call sdkgo.Call, input DeleteAdInput) sdkgo.MutationAttempt[AdMutationResult] {
	if !validAdvertisingID(input.AccountID) || !validAdvertisingID(input.AdGroupID) || !validAdvertisingID(input.AdID) {
		return keywordWriteBranch(DeleteAdBranchDefect, AdMutationResult{}, "deleteAd", sdkgo.FailureValidation, "ad IDs are invalid")
	}
	response := operation.client.requestCampaign(call, http.MethodDelete, adsPath, input.AccountID, map[string]any{"AdGroupId": input.AdGroupID, "AdIds": []string{input.AdID}})
	return adSafeWrite("deleteAd", DeleteAdBranchDeleted, DeleteAdBranchProviderRejected, DeleteAdBranchInvalidResponse, DeleteAdBranchDefect, input.AdID, response)
}

func adAssetLinks(copy []string) []any {
	links := make([]any, 0, len(copy))
	for _, text := range copy {
		links = append(links, map[string]any{"Asset": map[string]any{"Type": "TextAsset", "Text": text}})
	}
	return links
}

func validAdCopy(copy []string, minimum, maximum int) bool {
	if len(copy) < minimum || len(copy) > maximum {
		return false
	}
	seen := make(map[string]bool, len(copy))
	for _, text := range copy {
		if text == "" || strings.TrimSpace(text) != text || strings.ContainsAny(text, "\r\n") || len(text) > 1000 || seen[text] {
			return false
		}
		seen[text] = true
	}
	return true
}

func validAdURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && len(rawURL) <= 2048
}

func decodeAds(body []byte) (AdsPage, error) {
	var wire struct {
		Ads []struct {
			ID        string   `json:"Id"`
			Status    string   `json:"Status"`
			FinalURLs []string `json:"FinalUrls"`
		} `json:"Ads"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Ads == nil {
		return AdsPage{}, fmt.Errorf("ads are missing or malformed")
	}
	page := AdsPage{Ads: make([]AdSummary, 0, len(wire.Ads))}
	for _, ad := range wire.Ads {
		if !validAdvertisingID(ad.ID) || (ad.Status != "Active" && ad.Status != "Paused" && ad.Status != "Deleted") || len(ad.FinalURLs) == 0 || !validAdURL(ad.FinalURLs[0]) {
			return AdsPage{}, fmt.Errorf("ad identity is invalid")
		}
		page.Ads = append(page.Ads, AdSummary{ID: ad.ID, Status: ad.Status, FinalURL: ad.FinalURLs[0]})
	}
	return page, nil
}

func adSafeWrite(operation string, success, rejection, invalid, defect sdkgo.BranchID, adID string, response campaignHTTPResponse) sdkgo.MutationAttempt[AdMutationResult] {
	if response.credentialFailure {
		return keywordWriteBranch(defect, AdMutationResult{}, operation, sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
	}
	if response.shouldRetry() {
		return sdkgo.NewMutationRetry[AdMutationResult](campaignFailure(operation, response.failureKind(), "ad write is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return keywordWriteBranch(rejection, AdMutationResult{}, operation, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return keywordWriteBranch(invalid, AdMutationResult{}, operation, sdkgo.FailureProtocol, "ad response is unreadable")
	}
	if response.status == http.StatusOK && len(response.body) > 0 {
		var wire struct {
			PartialErrors []json.RawMessage `json:"PartialErrors"`
		}
		if json.Unmarshal(response.body, &wire) != nil {
			return keywordWriteBranch(invalid, AdMutationResult{}, operation, sdkgo.FailureProtocol, "ad response is invalid")
		}
		if len(wire.PartialErrors) != 0 {
			return keywordWriteBranch(rejection, AdMutationResult{}, operation, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected the ad")
		}
	}
	return sdkgo.NewMutationBranch(success, AdMutationResult{AdID: adID}, nil, sdkgo.Receipt{})
}
