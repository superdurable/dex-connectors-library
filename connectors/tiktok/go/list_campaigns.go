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

// ListCampaignsInput selects one advertiser campaign page.
type ListCampaignsInput struct {
	// AdvertiserID is the decimal ad account ID from TikTok Ads Manager.
	AdvertiserID string `json:"advertiserId"`
	// Page starts at one; zero uses one.
	Page int `json:"page,omitempty"`
	// PageSize accepts 1 through 1000 campaigns; zero uses ten.
	PageSize int `json:"pageSize,omitempty"`
}

// CampaignMetadata contains a stable campaign ID and display name.
type CampaignMetadata struct {
	// ID is the decimal campaign ID returned by TikTok.
	ID string `json:"id"`
	// Name is the campaign's display name.
	Name string `json:"name"`
}

// CampaignsPage contains one bounded campaign page and pagination metadata.
type CampaignsPage struct {
	// Campaigns are the campaign identities on this page.
	Campaigns []CampaignMetadata `json:"campaigns"`
	// Page is the one-based page number returned by TikTok.
	Page int `json:"page"`
	// TotalPages is zero when no campaign pages exist.
	TotalPages int `json:"totalPages"`
}

// ListCampaignsOperation reads one page of authorized campaigns.
type ListCampaignsOperation struct{ client *Client }

// ListCampaigns binds the campaign Query to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Invoke reads one page and classifies retryable and definitive outcomes.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListCampaignsInput) sdkgo.QueryAttempt[CampaignsPage] {
	if !validDecimalID(input.AdvertiserID) || input.Page < 0 || input.Page > 1000000 || input.PageSize < 0 || input.PageSize > 1000 {
		return campaignsBranch(ListCampaignsBranchDefect, sdkgo.FailureValidation, "advertiser ID or pagination is invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 10
	}
	query := url.Values{"advertiser_id": {input.AdvertiserID}, "page": {strconv.Itoa(input.Page)}, "page_size": {strconv.Itoa(input.PageSize)}}
	response := operation.client.get(call, "/campaign/get/?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return campaignsBranch(ListCampaignsBranchDefect, sdkgo.FailureAuthentication, "TikTok credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CampaignsPage](campaignsFailure(sdkgo.FailureTransport, "TikTok request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CampaignsPage](campaignsFailure(sdkgo.FailureRateLimit, "TikTok rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CampaignsPage](campaignsFailure(sdkgo.FailureAvailability, "TikTok is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return campaignsBranch(ListCampaignsBranchProviderRejected, sdkgo.FailureProviderRejection, "TikTok rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return campaignsBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "TikTok response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CampaignsPage](campaignsFailure(sdkgo.FailureTransport, "TikTok response was interrupted"), 0)
	}
	page, rejected, err := decodeCampaignsPage(response.body, input.Page, input.PageSize)
	if rejected {
		return campaignsBranch(ListCampaignsBranchProviderRejected, sdkgo.FailureProviderRejection, "TikTok rejected the campaign query")
	}
	if err != nil {
		return campaignsBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, "TikTok returned invalid campaign JSON")
	}
	return sdkgo.NewQueryBranch(ListCampaignsBranchListed, page, nil, sdkgo.Receipt{})
}

func validDecimalID(identifier string) bool {
	if len(identifier) == 0 || len(identifier) > 20 || identifier[0] == '0' {
		return false
	}
	for _, digit := range identifier {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func decodeCampaignsPage(body []byte, requestedPage, pageSize int) (CampaignsPage, bool, error) {
	var wire struct {
		Code *int `json:"code"`
		Data struct {
			List *[]struct {
				ID   string `json:"campaign_id"`
				Name string `json:"campaign_name"`
			} `json:"list"`
			PageInfo struct {
				Page      *int `json:"page"`
				TotalPage *int `json:"total_page"`
			} `json:"page_info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CampaignsPage{}, false, err
	}
	if wire.Code == nil {
		return CampaignsPage{}, false, fmt.Errorf("missing API code")
	}
	if *wire.Code != 0 {
		return CampaignsPage{}, true, nil
	}
	if wire.Data.List == nil || wire.Data.PageInfo.Page == nil || wire.Data.PageInfo.TotalPage == nil ||
		*wire.Data.PageInfo.Page != requestedPage || *wire.Data.PageInfo.TotalPage < 0 ||
		*wire.Data.PageInfo.TotalPage > 1000000 || len(*wire.Data.List) > pageSize {
		return CampaignsPage{}, false, fmt.Errorf("invalid campaign pagination")
	}
	campaigns := make([]CampaignMetadata, 0, len(*wire.Data.List))
	seen := make(map[string]struct{}, len(*wire.Data.List))
	for _, item := range *wire.Data.List {
		if !validDecimalID(item.ID) || strings.TrimSpace(item.Name) == "" {
			return CampaignsPage{}, false, fmt.Errorf("invalid campaign")
		}
		if _, exists := seen[item.ID]; exists {
			return CampaignsPage{}, false, fmt.Errorf("duplicate campaign")
		}
		seen[item.ID] = struct{}{}
		campaigns = append(campaigns, CampaignMetadata{ID: item.ID, Name: item.Name})
	}
	return CampaignsPage{Campaigns: campaigns, Page: requestedPage, TotalPages: *wire.Data.PageInfo.TotalPage}, false, nil
}

func campaignsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "tiktok-business", Operation: "listCampaigns", Message: message}
}

func campaignsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CampaignsPage] {
	failure := campaignsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CampaignsPage{}, &failure, sdkgo.Receipt{})
}
