// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiktok

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAdgroupsInput selects one advertiser ad group page.
type ListAdgroupsInput = ListCampaignsInput

// ListAdsInput selects one advertiser ad page.
type ListAdsInput = ListCampaignsInput

// AdvertisingResourceMetadata contains one TikTok ad group or ad identity.
type AdvertisingResourceMetadata struct {
	// ID is the decimal resource ID.
	ID string `json:"id"`
	// Name is the provider's display name.
	Name string `json:"name"`
}

// AdgroupsPage contains one bounded ad group page.
type AdgroupsPage struct {
	// Adgroups are the identities on this page.
	Adgroups []AdvertisingResourceMetadata `json:"adgroups"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// TotalPages is zero when no pages exist.
	TotalPages int `json:"totalPages"`
}

// AdsPage contains one bounded ad page.
type AdsPage struct {
	// Ads are the identities on this page.
	Ads []AdvertisingResourceMetadata `json:"ads"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// TotalPages is zero when no pages exist.
	TotalPages int `json:"totalPages"`
}

// ListAdgroupsOperation reads one page of ad groups.
type ListAdgroupsOperation struct{ client *Client }

// ListAdgroups binds the ad group Query to this client.
func (client *Client) ListAdgroups() ListAdgroupsOperation { return ListAdgroupsOperation{client} }

// Definition returns the generated ad group contract.
func (ListAdgroupsOperation) Definition() sdkgo.QueryDefinition { return ListAdgroupsDefinition }

// Invoke reads one ad group page and classifies provider outcomes.
func (operation ListAdgroupsOperation) Invoke(call sdkgo.Call, input ListAdgroupsInput) sdkgo.QueryAttempt[AdgroupsPage] {
	return invokeAdvertisingResourceList(call, operation.client, input, "adgroup", "adgroup_id", "adgroup_name", "listAdgroups", ListAdgroupsBranchListed, ListAdgroupsBranchProviderRejected, ListAdgroupsBranchInvalidResponse, ListAdgroupsBranchDefect,
		func(items []AdvertisingResourceMetadata, page, totalPages int) AdgroupsPage {
			return AdgroupsPage{Adgroups: items, Page: page, TotalPages: totalPages}
		})
}

// ListAdsOperation reads one page of ads.
type ListAdsOperation struct{ client *Client }

// ListAds binds the ad Query to this client.
func (client *Client) ListAds() ListAdsOperation { return ListAdsOperation{client} }

// Definition returns the generated ad contract.
func (ListAdsOperation) Definition() sdkgo.QueryDefinition { return ListAdsDefinition }

// Invoke reads one ad page and classifies provider outcomes.
func (operation ListAdsOperation) Invoke(call sdkgo.Call, input ListAdsInput) sdkgo.QueryAttempt[AdsPage] {
	return invokeAdvertisingResourceList(call, operation.client, input, "ad", "ad_id", "ad_name", "listAds", ListAdsBranchListed, ListAdsBranchProviderRejected, ListAdsBranchInvalidResponse, ListAdsBranchDefect,
		func(items []AdvertisingResourceMetadata, page, totalPages int) AdsPage {
			return AdsPage{Ads: items, Page: page, TotalPages: totalPages}
		})
}

func invokeAdvertisingResourceList[T any](call sdkgo.Call, client *Client, input ListCampaignsInput, resource, identifierField, nameField, operation string, listed, rejected, invalid, defect sdkgo.BranchID, output func([]AdvertisingResourceMetadata, int, int) T) sdkgo.QueryAttempt[T] {
	branch := func(branchID sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
		failure := sdkgo.Failure{Kind: kind, Provider: "tiktok-business", Operation: operation, Message: message}
		var zero T
		return sdkgo.NewQueryBranch(branchID, zero, &failure, sdkgo.Receipt{})
	}
	retry := func(kind sdkgo.FailureKind, message string, response providerResponse) sdkgo.QueryAttempt[T] {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "tiktok-business", Operation: operation, Message: message}, response.retryAfter)
	}
	if !validDecimalID(input.AdvertiserID) || input.Page < 0 || input.Page > 1000000 || input.PageSize < 0 || input.PageSize > 1000 {
		return branch(defect, sdkgo.FailureValidation, "advertiser ID or pagination is invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 10
	}
	query := url.Values{"advertiser_id": {input.AdvertiserID}, "page": {strconv.Itoa(input.Page)}, "page_size": {strconv.Itoa(input.PageSize)}}
	response := client.get(call, "/"+resource+"/get/?"+query.Encode())
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
		return retry(sdkgo.FailureAvailability, "TikTok is temporarily unavailable", response)
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
	items, totalPages, apiCode, err := decodeAdvertisingResourcePage(response.body, input.Page, input.PageSize, identifierField, nameField)
	if err != nil {
		return branch(invalid, sdkgo.FailureProtocol, "TikTok returned invalid "+resource+" JSON")
	}
	if apiCode == tiktokCodeRateLimited {
		return retry(sdkgo.FailureRateLimit, "TikTok rate limited the request", response)
	}
	if isTikTokSystemErrorCode(apiCode) {
		return retry(sdkgo.FailureAvailability, "TikTok is temporarily unavailable", response)
	}
	if apiCode != 0 {
		return branch(rejected, sdkgo.FailureProviderRejection, "TikTok rejected the query with code "+strconv.Itoa(apiCode))
	}
	return sdkgo.NewQueryBranch(listed, output(items, input.Page, totalPages), nil, sdkgo.Receipt{})
}

func decodeAdvertisingResourcePage(body []byte, requestedPage, pageSize int, identifierField, nameField string) ([]AdvertisingResourceMetadata, int, int, error) {
	var wire struct {
		Code *int `json:"code"`
		Data struct {
			List     *[]map[string]json.RawMessage `json:"list"`
			PageInfo struct {
				Page      *int `json:"page"`
				TotalPage *int `json:"total_page"`
			} `json:"page_info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, 0, 0, err
	}
	if wire.Code == nil {
		return nil, 0, 0, fmt.Errorf("missing API code")
	}
	if *wire.Code != 0 {
		return nil, 0, *wire.Code, nil
	}
	if wire.Data.List == nil || wire.Data.PageInfo.Page == nil || wire.Data.PageInfo.TotalPage == nil || *wire.Data.PageInfo.Page != requestedPage || *wire.Data.PageInfo.TotalPage < 0 || *wire.Data.PageInfo.TotalPage > 1000000 || len(*wire.Data.List) > pageSize {
		return nil, 0, 0, fmt.Errorf("invalid pagination")
	}
	items := make([]AdvertisingResourceMetadata, 0, len(*wire.Data.List))
	seen := make(map[string]struct{}, len(*wire.Data.List))
	for _, item := range *wire.Data.List {
		var identifier, name string
		if json.Unmarshal(item[identifierField], &identifier) != nil || json.Unmarshal(item[nameField], &name) != nil || !validDecimalID(identifier) || strings.TrimSpace(name) == "" {
			return nil, 0, 0, fmt.Errorf("invalid resource")
		}
		if _, exists := seen[identifier]; exists {
			return nil, 0, 0, fmt.Errorf("duplicate resource")
		}
		seen[identifier] = struct{}{}
		items = append(items, AdvertisingResourceMetadata{ID: identifier, Name: name})
	}
	return items, *wire.Data.PageInfo.TotalPage, 0, nil
}
