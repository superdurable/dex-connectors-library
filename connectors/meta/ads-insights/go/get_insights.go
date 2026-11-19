// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package metaadsinsights

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetInsightsInput selects one ad object's performance page.
type GetInsightsInput struct {
	// ObjectID is an ad account ID with act_ prefix, or a numeric campaign, ad set, or ad ID.
	ObjectID string `json:"objectId"`
	// DatePreset is a Meta date preset such as last_7d; blank uses the provider's reporting window.
	DatePreset string `json:"datePreset"`
	// Limit is the maximum rows in one page, from 1 to 500; zero uses Meta's default.
	Limit int `json:"limit"`
	// After is a cursor from a prior InsightsPage; blank starts at the first page.
	After string `json:"after"`
}

// InsightMetrics contains the basic metrics and IDs in one Meta insights row.
type InsightMetrics struct {
	// AccountID is the advertising account's numeric ID.
	AccountID string `json:"accountId"`
	// CampaignID is present for campaign and lower-level rows.
	CampaignID string `json:"campaignId"`
	// AdSetID is present for ad set and ad rows.
	AdSetID string `json:"adSetId"`
	// AdID is present for ad rows.
	AdID string `json:"adId"`
	// DateStart is the reporting period's first date in YYYY-MM-DD format.
	DateStart string `json:"dateStart"`
	// DateStop is the reporting period's last date in YYYY-MM-DD format.
	DateStop string `json:"dateStop"`
	// Impressions is Meta's decimal text count.
	Impressions string `json:"impressions"`
	// Clicks is Meta's decimal text count.
	Clicks string `json:"clicks"`
	// Spend is Meta's decimal text amount in the ad account currency.
	Spend string `json:"spend"`
}

// InsightsPage holds one ordered page and a cursor for a following page.
type InsightsPage struct {
	// Rows are the metrics returned by Meta.
	Rows []InsightMetrics `json:"rows"`
	// NextCursor is blank when Meta returned no next-page link.
	NextCursor string `json:"nextCursor"`
}

// GetInsightsOperation reads performance metrics for an ad object.
type GetInsightsOperation struct{ client *Client }

// GetInsights binds the Query to this client.
func (client *Client) GetInsights() GetInsightsOperation { return GetInsightsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetInsightsOperation) Definition() sdkgo.QueryDefinition { return GetInsightsDefinition }

var insightsObjectID = regexp.MustCompile(`^(act_)?[0-9]{1,40}$`)
var insightsDatePreset = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
var insightsCursor = regexp.MustCompile(`^[A-Za-z0-9_+=/-]{1,1000}$`)
var insightsDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
var insightsCount = regexp.MustCompile(`^[0-9]+$`)
var insightsAmount = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// Invoke requests basic metrics without exposing the access token in the URL or result.
func (operation GetInsightsOperation) Invoke(call sdkgo.Call, input GetInsightsInput) sdkgo.QueryAttempt[InsightsPage] {
	if !insightsObjectID.MatchString(input.ObjectID) || input.Limit < 0 || input.Limit > 500 ||
		(input.DatePreset != "" && !insightsDatePreset.MatchString(input.DatePreset)) ||
		(input.After != "" && !insightsCursor.MatchString(input.After)) {
		return insightsBranch(GetInsightsBranchDefect, sdkgo.FailureValidation, "ad object ID, date preset, limit, or cursor is invalid")
	}
	query := url.Values{"fields": {"account_id,campaign_id,adset_id,ad_id,date_start,date_stop,impressions,clicks,spend"}}
	if input.DatePreset != "" {
		query.Set("date_preset", input.DatePreset)
	}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.After != "" {
		query.Set("after", input.After)
	}
	response := operation.client.get(call, "/"+input.ObjectID+"/insights?"+query.Encode())
	if decision, handled := classifyGraphResponse("getInsights", response, InsightsPage{}, GetInsightsBranchNotFound, GetInsightsBranchProviderRejected, GetInsightsBranchInvalidResponse, GetInsightsBranchDefect); handled {
		return decision
	}
	var envelope struct {
		Data []struct {
			AccountID   string `json:"account_id"`
			CampaignID  string `json:"campaign_id"`
			AdSetID     string `json:"adset_id"`
			AdID        string `json:"ad_id"`
			DateStart   string `json:"date_start"`
			DateStop    string `json:"date_stop"`
			Impressions string `json:"impressions"`
			Clicks      string `json:"clicks"`
			Spend       string `json:"spend"`
		} `json:"data"`
		Paging struct {
			Next    string `json:"next"`
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
		} `json:"paging"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Data == nil {
		return insightsBranch(GetInsightsBranchInvalidResponse, sdkgo.FailureProtocol, "Meta returned invalid insights JSON")
	}
	rows := make([]InsightMetrics, 0, len(envelope.Data))
	for _, row := range envelope.Data {
		if row.AccountID == "" || !insightsDate.MatchString(row.DateStart) || !insightsDate.MatchString(row.DateStop) ||
			!insightsCount.MatchString(row.Impressions) || !insightsCount.MatchString(row.Clicks) ||
			!insightsAmount.MatchString(row.Spend) {
			return insightsBranch(GetInsightsBranchInvalidResponse, sdkgo.FailureProtocol, "Meta returned invalid insights metrics")
		}
		rows = append(rows, InsightMetrics{AccountID: row.AccountID, CampaignID: row.CampaignID, AdSetID: row.AdSetID, AdID: row.AdID,
			DateStart: row.DateStart, DateStop: row.DateStop, Impressions: row.Impressions, Clicks: row.Clicks, Spend: row.Spend})
	}
	nextCursor := ""
	if envelope.Paging.Next != "" {
		nextCursor = envelope.Paging.Cursors.After
		if !insightsCursor.MatchString(nextCursor) {
			return insightsBranch(GetInsightsBranchInvalidResponse, sdkgo.FailureProtocol, "Meta returned an invalid insights cursor")
		}
	}
	return sdkgo.NewQueryBranch(GetInsightsBranchListed, InsightsPage{Rows: rows, NextCursor: nextCursor}, nil, sdkgo.Receipt{})
}

func insightsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[InsightsPage] {
	failure := queryFailure("getInsights", kind, message)
	return sdkgo.NewQueryBranch(branch, InsightsPage{}, &failure, sdkgo.Receipt{})
}
