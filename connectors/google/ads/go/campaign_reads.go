// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Campaign is a Google Ads campaign summary.
type Campaign struct {
	// ResourceName is customers/{customer}/campaigns/{campaign}.
	ResourceName string `json:"resourceName"`
	// ID is the provider campaign ID.
	ID string `json:"id"`
	// Name is the campaign display name.
	Name string `json:"name"`
	// Status is ENABLED, PAUSED, or REMOVED.
	Status string `json:"status"`
	// AdvertisingChannelType is the campaign's channel.
	AdvertisingChannelType string `json:"advertisingChannelType,omitempty"`
	// CampaignBudget is the associated budget resource name.
	CampaignBudget string `json:"campaignBudget,omitempty"`
}

// CampaignMetrics contains all-time or selected-period campaign totals.
type CampaignMetrics struct {
	// Impressions is the number of ad impressions.
	Impressions string `json:"impressions,omitempty"`
	// Clicks is the number of ad clicks.
	Clicks string `json:"clicks,omitempty"`
	// CostMicros is spend in millionths of the account currency.
	CostMicros string `json:"costMicros,omitempty"`
}

// CampaignRow combines one campaign with performance totals.
type CampaignRow struct {
	// Campaign is the selected campaign.
	Campaign Campaign `json:"campaign"`
	// Metrics contains selected totals.
	Metrics CampaignMetrics `json:"metrics"`
}

// CampaignPage contains one page of campaign search results.
type CampaignPage struct {
	// Rows are returned campaigns.
	Rows []CampaignRow `json:"rows"`
	// NextPageToken requests the next page; blank means no more pages.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListCampaignsInput selects an Ads account and optional filters.
type ListCampaignsInput struct {
	// CustomerID is the unhyphenated numeric client account ID.
	CustomerID string `json:"customerId"`
	// ManagerCustomerID is the unhyphenated manager ID when accessing a client account; blank omits the header.
	ManagerCustomerID string `json:"managerCustomerId,omitempty"`
	// Status optionally limits campaigns to ENABLED, PAUSED, or REMOVED.
	Status string `json:"status,omitempty"`
	// DateRange optionally limits metrics to TODAY, YESTERDAY, LAST_7_DAYS, LAST_30_DAYS, THIS_MONTH, or LAST_MONTH.
	DateRange string `json:"dateRange,omitempty"`
	// PageToken is the previous result's nextPageToken; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// GetCampaignInput selects one campaign by customer and campaign IDs.
type GetCampaignInput struct {
	// CustomerID is the unhyphenated numeric client account ID.
	CustomerID string `json:"customerId"`
	// ManagerCustomerID is the unhyphenated manager ID when needed; blank omits the header.
	ManagerCustomerID string `json:"managerCustomerId,omitempty"`
	// CampaignID is the numeric campaign ID shown in Google Ads.
	CampaignID string `json:"campaignId"`
}

// ListCampaignsOperation searches one page of campaigns.
type ListCampaignsOperation struct{ client *Client }

// GetCampaignOperation reads one campaign.
type GetCampaignOperation struct{ client *Client }

// ListCampaigns binds the campaign listing Query to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// GetCampaign binds the campaign lookup Query to this client.
func (client *Client) GetCampaign() GetCampaignOperation { return GetCampaignOperation{client} }

// Definition returns the generated campaign listing contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Definition returns the generated campaign lookup contract.
func (GetCampaignOperation) Definition() sdkgo.QueryDefinition { return GetCampaignDefinition }

var numericAdsID = regexp.MustCompile(`^[0-9]{1,20}$`)

const campaignSelect = "SELECT campaign.resource_name, campaign.id, campaign.name, campaign.status, campaign.advertising_channel_type, campaign.campaign_budget, metrics.impressions, metrics.clicks, metrics.cost_micros FROM campaign"

// Invoke reads one campaign page through Google Ads Search.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListCampaignsInput) sdkgo.QueryAttempt[CampaignPage] {
	if !validAdsIDs(input.CustomerID, input.ManagerCustomerID) || (input.Status != "" && input.Status != "ENABLED" && input.Status != "PAUSED" && input.Status != "REMOVED") || (input.DateRange != "" && input.DateRange != "TODAY" && input.DateRange != "YESTERDAY" && input.DateRange != "LAST_7_DAYS" && input.DateRange != "LAST_30_DAYS" && input.DateRange != "THIS_MONTH" && input.DateRange != "LAST_MONTH") || len(input.PageToken) > 2048 {
		return campaignQueryBranch(ListCampaignsBranchDefect, CampaignPage{}, "listCampaigns", sdkgo.FailureValidation, "customer ID or filter is invalid")
	}
	query := campaignSelect + " WHERE campaign.id > 0"
	if input.Status != "" {
		query += " AND campaign.status = " + input.Status
	}
	if input.DateRange != "" {
		query += " AND segments.date DURING " + input.DateRange
	}
	requestBody, _ := json.Marshal(map[string]string{"query": query, "pageToken": input.PageToken})
	response := operation.client.post(call, campaignSearchPath(input.CustomerID), requestBody, input.ManagerCustomerID)
	if failure, retry, handled := campaignQueryResponse[CampaignPage](response, "listCampaigns", ListCampaignsBranchProviderRejected, ListCampaignsBranchInvalidResponse, ListCampaignsBranchDefect); handled {
		if retry != nil {
			return *retry
		}
		return *failure
	}
	page, err := decodeCampaignPage(response.body)
	if err != nil {
		return campaignQueryBranch(ListCampaignsBranchInvalidResponse, CampaignPage{}, "listCampaigns", sdkgo.FailureProtocol, "Google Ads returned invalid campaign JSON")
	}
	return sdkgo.NewQueryBranch(ListCampaignsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one campaign by numeric ID through Google Ads Search.
func (operation GetCampaignOperation) Invoke(call sdkgo.Call, input GetCampaignInput) sdkgo.QueryAttempt[CampaignRow] {
	if !validAdsIDs(input.CustomerID, input.ManagerCustomerID) || !numericAdsID.MatchString(input.CampaignID) {
		return campaignQueryBranch(GetCampaignBranchDefect, CampaignRow{}, "getCampaign", sdkgo.FailureValidation, "customer, manager, or campaign ID is invalid")
	}
	requestBody, _ := json.Marshal(map[string]string{"query": campaignSelect + " WHERE campaign.id = " + input.CampaignID})
	response := operation.client.post(call, campaignSearchPath(input.CustomerID), requestBody, input.ManagerCustomerID)
	if failure, retry, handled := campaignQueryResponse[CampaignRow](response, "getCampaign", GetCampaignBranchProviderRejected, GetCampaignBranchInvalidResponse, GetCampaignBranchDefect); handled {
		if retry != nil {
			return *retry
		}
		return *failure
	}
	page, err := decodeCampaignPage(response.body)
	if err != nil {
		return campaignQueryBranch(GetCampaignBranchInvalidResponse, CampaignRow{}, "getCampaign", sdkgo.FailureProtocol, "Google Ads returned invalid campaign JSON")
	}
	if len(page.Rows) == 0 {
		return campaignQueryBranch(GetCampaignBranchNotFound, CampaignRow{}, "getCampaign", sdkgo.FailureNotFound, "campaign was not found")
	}
	if len(page.Rows) != 1 {
		return campaignQueryBranch(GetCampaignBranchInvalidResponse, CampaignRow{}, "getCampaign", sdkgo.FailureProtocol, "Google Ads returned multiple matching campaigns")
	}
	return sdkgo.NewQueryBranch(GetCampaignBranchFound, page.Rows[0], nil, sdkgo.Receipt{})
}

func validAdsIDs(customerID, managerCustomerID string) bool {
	return numericAdsID.MatchString(customerID) && (managerCustomerID == "" || numericAdsID.MatchString(managerCustomerID))
}

func campaignSearchPath(customerID string) string {
	return "/v26/customers/" + customerID + "/googleAds:search"
}

func decodeCampaignPage(body []byte) (CampaignPage, error) {
	var response struct {
		Results       []CampaignRow `json:"results"`
		NextPageToken string        `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return CampaignPage{}, err
	}
	if response.Results == nil {
		return CampaignPage{}, fmt.Errorf("missing campaign results")
	}
	if len(response.Results) > 10000 {
		return CampaignPage{}, fmt.Errorf("campaign page exceeds API limit")
	}
	for _, row := range response.Results {
		if row.Campaign.ResourceName == "" || !strings.HasPrefix(row.Campaign.ResourceName, "customers/") {
			return CampaignPage{}, fmt.Errorf("invalid campaign name")
		}
	}
	return CampaignPage{Rows: response.Results, NextPageToken: response.NextPageToken}, nil
}

func campaignQueryBranch[T any](branch sdkgo.BranchID, zero T, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-ads", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func campaignQueryResponse[T any](response providerResponse, operation string, rejected, invalid, defect sdkgo.BranchID) (*sdkgo.QueryAttempt[T], *sdkgo.QueryAttempt[T], bool) {
	var zero T
	if errors.Is(response.err, errCredential) {
		answer := campaignQueryBranch(defect, zero, operation, sdkgo.FailureAuthentication, "connection credentials are unavailable")
		return &answer, nil, true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		answer := campaignQueryBranch(invalid, zero, operation, sdkgo.FailureResponseTooLarge, "Google Ads response exceeded the limit")
		return &answer, nil, true
	}
	if response.err != nil {
		answer := sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "google-ads", Operation: operation, Message: "Google Ads request failed"}, 0)
		return nil, &answer, true
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		answer := sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "google-ads", Operation: operation, Message: "Google Ads asked to retry"}, response.retryAfter)
		return nil, &answer, true
	}
	if response.status != http.StatusOK {
		answer := campaignQueryBranch(rejected, zero, operation, sdkgo.FailureProviderRejection, "Google Ads rejected HTTP "+strconv.Itoa(response.status))
		return &answer, nil, true
	}
	return nil, nil, false
}
