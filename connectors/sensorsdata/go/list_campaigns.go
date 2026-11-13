// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sensorsdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const campaignsPath = "/api/v3/analytics/v1/channel/campaigns/list"

// ListCampaignsInput selects a zero-based campaign page; zero size uses 20.
type ListCampaignsInput struct {
	// PageNum is the zero-based page number.
	PageNum int `json:"pageNum"`
	// PageSize is the number of campaigns requested, from 1 to 1000.
	PageSize int `json:"pageSize"`
}

// Campaign contains only channel campaign metadata.
type Campaign struct {
	// Name is the campaign name.
	Name string `json:"name"`
	// LinkCount is the number of channel links under the campaign.
	LinkCount int64 `json:"linkCount"`
	// LatestUseTime is the provider's last channel creation time.
	LatestUseTime string `json:"latestUseTime"`
}

// ListCampaignsOutput contains one page and its pagination metadata.
type ListCampaignsOutput struct {
	// Campaigns are the returned campaign metadata in provider order.
	Campaigns []Campaign `json:"campaigns"`
	// TotalRows is the total number of campaigns.
	TotalRows int64 `json:"totalRows"`
	// TotalPages is the total number of pages.
	TotalPages int `json:"totalPages"`
	// PageNum is the returned zero-based page number.
	PageNum int `json:"pageNum"`
	// PageSize is the returned page size.
	PageSize int `json:"pageSize"`
}

// ListCampaignsOperation reads a Sensors Data campaign page.
type ListCampaignsOperation struct{ client *Client }

// Definition returns the generated campaign-list contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Invoke lists campaign metadata and classifies provider and transport outcomes.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListCampaignsInput) sdkgo.QueryAttempt[ListCampaignsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return campaignCatalogBranch(ListCampaignsBranchDefect, sdkgo.FailureAuthentication, "Sensors Data API Key is unavailable or invalid")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 20
	}
	if input.PageNum < 0 || input.PageNum > 100000 || pageSize < 1 || pageSize > 1000 {
		return campaignCatalogBranch(ListCampaignsBranchDefect, sdkgo.FailureValidation, "pageNum or pageSize is outside the supported range")
	}
	body, err := json.Marshal(struct {
		PageNum  int `json:"page_num"`
		PageSize int `json:"page_size"`
	}{input.PageNum, pageSize})
	if err != nil {
		return campaignCatalogBranch(ListCampaignsBranchDefect, sdkgo.FailureValidation, "campaign page is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+campaignsPath, bytes.NewReader(body))
	if err != nil {
		return campaignCatalogBranch(ListCampaignsBranchDefect, sdkgo.FailureValidation, "campaign request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("api-key", credentials.APIKey.Reveal())
	request.Header.Set("sensorsdata-project", operation.client.projectName)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return campaignCatalogRetry(sdkgo.FailureAvailability, "Sensors Data request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return campaignCatalogRetry(sdkgo.FailureRateLimit, "Sensors Data rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return campaignCatalogRetry(sdkgo.FailureAvailability, "Sensors Data temporarily unavailable", retryAfter)
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return campaignCatalogBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "campaign response exceeded the configured limit")
	}
	if err != nil {
		return campaignCatalogRetry(sdkgo.FailureAvailability, "campaign response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return campaignCatalogBranch(ListCampaignsBranchProviderRejected, kind, "Sensors Data rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code string `json:"code"`
		Data *struct {
			CampaignDetails []struct {
				Name          string `json:"campaign_name"`
				LinkCount     *int64 `json:"branch_num"`
				LatestUseTime string `json:"latest_use_time"`
			} `json:"campaign_details"`
			TotalRows  json.RawMessage `json:"total_rows"`
			TotalPages *int            `json:"total_page"`
			PageNum    *int            `json:"page_num"`
			PageSize   *int            `json:"page_size"`
		} `json:"data"`
	}
	if json.Unmarshal(responseBody, &wire) != nil || wire.Code == "" {
		return campaignCatalogBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, "Sensors Data returned invalid campaign data")
	}
	if wire.Code != "SUCCESS" {
		return campaignCatalogBranch(ListCampaignsBranchProviderRejected, sdkgo.FailureProviderRejection, "Sensors Data rejected the campaign query")
	}
	if wire.Data == nil || wire.Data.TotalPages == nil || wire.Data.PageNum == nil || wire.Data.PageSize == nil || *wire.Data.TotalPages < 0 || *wire.Data.PageNum < 0 || *wire.Data.PageSize < 1 || *wire.Data.PageSize > 1000 {
		return campaignCatalogBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, "Sensors Data returned invalid pagination data")
	}
	totalRows, err := decodeTotalRows(wire.Data.TotalRows)
	if err != nil || totalRows < 0 || len(wire.Data.CampaignDetails) > pageSize {
		return campaignCatalogBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, "Sensors Data returned invalid campaign count")
	}
	campaigns := make([]Campaign, 0, len(wire.Data.CampaignDetails))
	for _, item := range wire.Data.CampaignDetails {
		if item.Name == "" || len(item.Name) > 1024 || item.LinkCount == nil || *item.LinkCount < 0 || len(item.LatestUseTime) > 64 {
			return campaignCatalogBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, "Sensors Data returned invalid campaign metadata")
		}
		campaigns = append(campaigns, Campaign{Name: item.Name, LinkCount: *item.LinkCount, LatestUseTime: item.LatestUseTime})
	}
	return sdkgo.NewQueryBranch(ListCampaignsBranchListed, ListCampaignsOutput{Campaigns: campaigns, TotalRows: totalRows, TotalPages: *wire.Data.TotalPages, PageNum: *wire.Data.PageNum, PageSize: *wire.Data.PageSize}, nil, sdkgo.Receipt{})
}
func decodeTotalRows(raw json.RawMessage) (int64, error) {
	var text string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
	} else {
		text = string(raw)
	}
	return strconv.ParseInt(text, 10, 64)
}
func isValidAPIKey(value string) bool {
	if len(value) != 35 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func isValidProjectName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func campaignCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListCampaignsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "sensorsdata", Operation: "listCampaigns", Message: message}
	return sdkgo.NewQueryBranch(branch, ListCampaignsOutput{}, &failure, sdkgo.Receipt{})
}
func campaignCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListCampaignsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "sensorsdata", Operation: "listCampaigns", Message: message}
	return sdkgo.NewQueryRetry[ListCampaignsOutput](failure, delay)
}
