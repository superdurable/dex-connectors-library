// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package metaadsinsights

import (
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListMarketingObjectsInput selects one ad account collection and cursor page.
type ListMarketingObjectsInput struct {
	// AccountID is the numeric ad account ID from Meta Ads Manager, without act_.
	AccountID string `json:"accountId"`
	// Limit is the maximum objects in one page, from 1 to 500; zero uses Meta's default.
	Limit int `json:"limit"`
	// After is the opaque cursor from a prior page; blank starts at the first page.
	After string `json:"after"`
}

// MarketingObjectsPage contains one collection page and its next cursor.
type MarketingObjectsPage struct {
	// Objects are the ad, ad set, or campaign metadata in provider order.
	Objects []AdMetadata `json:"objects"`
	// NextCursor is blank when there is no next page.
	NextCursor string `json:"nextCursor"`
}

// ListAdsOperation lists ads in an account.
type ListAdsOperation struct{ client *Client }

// ListAds binds the ads Query to this client.
func (client *Client) ListAds() ListAdsOperation { return ListAdsOperation{client} }

// Definition returns the manifest-generated ads Query contract.
func (ListAdsOperation) Definition() sdkgo.QueryDefinition { return ListAdsDefinition }

// Invoke reads one page of ads.
func (operation ListAdsOperation) Invoke(call sdkgo.Call, input ListMarketingObjectsInput) sdkgo.QueryAttempt[MarketingObjectsPage] {
	return operation.client.listMarketingObjects(call, input, "listAds", "ads", ListAdsBranchListed, ListAdsBranchNotFound, ListAdsBranchProviderRejected, ListAdsBranchInvalidResponse, ListAdsBranchDefect)
}

// ListAdSetsOperation lists ad sets in an account.
type ListAdSetsOperation struct{ client *Client }

// ListAdSets binds the ad sets Query to this client.
func (client *Client) ListAdSets() ListAdSetsOperation { return ListAdSetsOperation{client} }

// Definition returns the manifest-generated ad sets Query contract.
func (ListAdSetsOperation) Definition() sdkgo.QueryDefinition { return ListAdSetsDefinition }

// Invoke reads one page of ad sets.
func (operation ListAdSetsOperation) Invoke(call sdkgo.Call, input ListMarketingObjectsInput) sdkgo.QueryAttempt[MarketingObjectsPage] {
	return operation.client.listMarketingObjects(call, input, "listAdSets", "adsets", ListAdSetsBranchListed, ListAdSetsBranchNotFound, ListAdSetsBranchProviderRejected, ListAdSetsBranchInvalidResponse, ListAdSetsBranchDefect)
}

// ListCampaignsOperation lists campaigns in an account.
type ListCampaignsOperation struct{ client *Client }

// ListCampaigns binds the campaigns Query to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// Definition returns the manifest-generated campaigns Query contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Invoke reads one page of campaigns.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListMarketingObjectsInput) sdkgo.QueryAttempt[MarketingObjectsPage] {
	return operation.client.listMarketingObjects(call, input, "listCampaigns", "campaigns", ListCampaignsBranchListed, ListCampaignsBranchNotFound, ListCampaignsBranchProviderRejected, ListCampaignsBranchInvalidResponse, ListCampaignsBranchDefect)
}

func (client *Client) listMarketingObjects(call sdkgo.Call, input ListMarketingObjectsInput, operationName, collection string, listed, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.QueryAttempt[MarketingObjectsPage] {
	if !numericID.MatchString(input.AccountID) || input.Limit < 0 || input.Limit > 500 || (input.After != "" && !insightsCursor.MatchString(input.After)) {
		return queryBranch(operationName, defect, MarketingObjectsPage{}, sdkgo.FailureValidation, "ad account ID, limit, or cursor is invalid")
	}
	query := url.Values{"fields": {"id,name,status,effective_status"}}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.After != "" {
		query.Set("after", input.After)
	}
	response := client.get(call, "/act_"+input.AccountID+"/"+collection+"?"+query.Encode())
	if decision, handled := classifyGraphResponse(operationName, response, MarketingObjectsPage{}, notFound, rejected, invalid, defect); handled {
		return decision
	}
	var envelope struct {
		Data   []adWire `json:"data"`
		Paging struct {
			Next    string `json:"next"`
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
		} `json:"paging"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Data == nil {
		return queryBranch(operationName, invalid, MarketingObjectsPage{}, sdkgo.FailureProtocol, "Meta returned invalid collection JSON")
	}
	objects := make([]AdMetadata, 0, len(envelope.Data))
	for _, item := range envelope.Data {
		metadata, valid := projectAd(item, item.ID)
		if !valid || !numericID.MatchString(item.ID) {
			return queryBranch(operationName, invalid, MarketingObjectsPage{}, sdkgo.FailureProtocol, "Meta returned incomplete collection metadata")
		}
		objects = append(objects, metadata)
	}
	nextCursor := ""
	if envelope.Paging.Next != "" {
		nextCursor = envelope.Paging.Cursors.After
		if !insightsCursor.MatchString(nextCursor) {
			return queryBranch(operationName, invalid, MarketingObjectsPage{}, sdkgo.FailureProtocol, "Meta returned an invalid collection cursor")
		}
	}
	return sdkgo.NewQueryBranch(listed, MarketingObjectsPage{Objects: objects, NextCursor: nextCursor}, nil, sdkgo.Receipt{})
}
