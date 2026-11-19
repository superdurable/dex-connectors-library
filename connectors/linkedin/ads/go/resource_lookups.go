// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

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

// GetAdAccountInput identifies one Advertising account.
type GetAdAccountInput struct {
	// ID is the decimal account ID from listAdAccounts.
	ID string `json:"id"`
}

// GetCampaignGroupInput identifies one campaign group in an account.
type GetCampaignGroupInput struct {
	// AccountID is the decimal Advertising account ID.
	AccountID string `json:"accountId"`
	// GroupID is the decimal campaign group ID.
	GroupID string `json:"groupId"`
}

// GetCampaignInput identifies one campaign in an account.
type GetCampaignInput struct {
	// AccountID is the decimal Advertising account ID.
	AccountID string `json:"accountId"`
	// CampaignID is the decimal campaign ID.
	CampaignID string `json:"campaignId"`
}

// GetCreativeInput identifies one creative in an account.
type GetCreativeInput struct {
	// AccountID is the decimal Advertising account ID.
	AccountID string `json:"accountId"`
	// CreativeURN is the full sponsoredCreative URN from listCreatives.
	CreativeURN string `json:"creativeUrn"`
}

// GetAdAccountOperation reads one Advertising account.
type GetAdAccountOperation struct{ client *Client }

// GetAdAccount binds the account Query to this client.
func (client *Client) GetAdAccount() GetAdAccountOperation { return GetAdAccountOperation{client} }

// Definition returns the generated Query contract.
func (GetAdAccountOperation) Definition() sdkgo.QueryDefinition { return GetAdAccountDefinition }

// Invoke reads one account's stable ID and name.
func (operation GetAdAccountOperation) Invoke(call sdkgo.Call, input GetAdAccountInput) sdkgo.QueryAttempt[AdAccount] {
	if !isDecimalID(input.ID) {
		return queryBranch[AdAccount](GetAdAccountBranchDefect, sdkgo.FailureValidation, "account ID is invalid", "getAdAccount")
	}
	return readLinkedInResource(operation.client.get(call, "/rest/adAccounts/"+input.ID), "getAdAccount", GetAdAccountBranchFound, GetAdAccountBranchNotFound, GetAdAccountBranchProviderRejected, GetAdAccountBranchInvalidResponse, GetAdAccountBranchDefect, decodeAdAccount)
}

// GetCampaignGroupOperation reads one campaign group.
type GetCampaignGroupOperation struct{ client *Client }

// GetCampaignGroup binds the group Query to this client.
func (client *Client) GetCampaignGroup() GetCampaignGroupOperation {
	return GetCampaignGroupOperation{client}
}

// Definition returns the generated Query contract.
func (GetCampaignGroupOperation) Definition() sdkgo.QueryDefinition {
	return GetCampaignGroupDefinition
}

// Invoke reads one group's ID, name, and status.
func (operation GetCampaignGroupOperation) Invoke(call sdkgo.Call, input GetCampaignGroupInput) sdkgo.QueryAttempt[CampaignGroup] {
	if !isDecimalID(input.AccountID) || !isDecimalID(input.GroupID) {
		return queryBranch[CampaignGroup](GetCampaignGroupBranchDefect, sdkgo.FailureValidation, "account or group ID is invalid", "getCampaignGroup")
	}
	path := "/rest/adAccounts/" + input.AccountID + "/adCampaignGroups/" + input.GroupID
	return readLinkedInResource(operation.client.get(call, path), "getCampaignGroup", GetCampaignGroupBranchFound, GetCampaignGroupBranchNotFound, GetCampaignGroupBranchProviderRejected, GetCampaignGroupBranchInvalidResponse, GetCampaignGroupBranchDefect, decodeCampaignGroup)
}

// GetCampaignOperation reads one campaign.
type GetCampaignOperation struct{ client *Client }

// GetCampaign binds the campaign Query to this client.
func (client *Client) GetCampaign() GetCampaignOperation { return GetCampaignOperation{client} }

// Definition returns the generated Query contract.
func (GetCampaignOperation) Definition() sdkgo.QueryDefinition { return GetCampaignDefinition }

// Invoke reads one campaign's ID, name, and status.
func (operation GetCampaignOperation) Invoke(call sdkgo.Call, input GetCampaignInput) sdkgo.QueryAttempt[Campaign] {
	if !isDecimalID(input.AccountID) || !isDecimalID(input.CampaignID) {
		return queryBranch[Campaign](GetCampaignBranchDefect, sdkgo.FailureValidation, "account or campaign ID is invalid", "getCampaign")
	}
	path := "/rest/adAccounts/" + input.AccountID + "/adCampaigns/" + input.CampaignID
	return readLinkedInResource(operation.client.get(call, path), "getCampaign", GetCampaignBranchFound, GetCampaignBranchNotFound, GetCampaignBranchProviderRejected, GetCampaignBranchInvalidResponse, GetCampaignBranchDefect, decodeCampaign)
}

// GetCreativeOperation reads one creative.
type GetCreativeOperation struct{ client *Client }

// GetCreative binds the creative Query to this client.
func (client *Client) GetCreative() GetCreativeOperation { return GetCreativeOperation{client} }

// Definition returns the generated Query contract.
func (GetCreativeOperation) Definition() sdkgo.QueryDefinition { return GetCreativeDefinition }

// Invoke reads one creative's ID, campaign, and intended status.
func (operation GetCreativeOperation) Invoke(call sdkgo.Call, input GetCreativeInput) sdkgo.QueryAttempt[Creative] {
	if !isDecimalID(input.AccountID) || !isSponsoredCreativeURN(input.CreativeURN) {
		return queryBranch[Creative](GetCreativeBranchDefect, sdkgo.FailureValidation, "account ID or creative URN is invalid", "getCreative")
	}
	path := "/rest/adAccounts/" + input.AccountID + "/creatives/" + url.PathEscape(input.CreativeURN)
	return readLinkedInResource(operation.client.get(call, path), "getCreative", GetCreativeBranchFound, GetCreativeBranchNotFound, GetCreativeBranchProviderRejected, GetCreativeBranchInvalidResponse, GetCreativeBranchDefect, decodeCreative)
}

func readLinkedInResource[Output any](response providerResponse, operation string, found, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (Output, error)) sdkgo.QueryAttempt[Output] {
	if errors.Is(response.err, errCredential) {
		return queryBranch[Output](defect, sdkgo.FailureAuthentication, "connection credentials unavailable", operation)
	}
	if errors.Is(response.err, errRequest) {
		return queryRetry[Output](sdkgo.FailureTransport, "LinkedIn Ads request failed", 0, operation)
	}
	if response.status == http.StatusTooManyRequests {
		return queryRetry[Output](sdkgo.FailureRateLimit, "LinkedIn Ads rate limited the request", response.retryAfter, operation)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return queryRetry[Output](sdkgo.FailureAvailability, "LinkedIn Ads is temporarily unavailable", response.retryAfter, operation)
	}
	if response.status == http.StatusNotFound {
		return queryBranch[Output](notFound, sdkgo.FailureNotFound, "LinkedIn Ads did not find the resource", operation)
	}
	if response.status != http.StatusOK {
		return queryBranch[Output](rejected, sdkgo.FailureProviderRejection, "LinkedIn Ads rejected HTTP "+strconv.Itoa(response.status), operation)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch[Output](invalid, sdkgo.FailureResponseTooLarge, "LinkedIn Ads response exceeded the limit", operation)
	}
	if response.err != nil {
		return queryRetry[Output](sdkgo.FailureTransport, "LinkedIn Ads response was interrupted", 0, operation)
	}
	output, err := decode(response.body)
	if err != nil {
		return queryBranch[Output](invalid, sdkgo.FailureProtocol, "LinkedIn Ads returned invalid resource JSON", operation)
	}
	return sdkgo.NewQueryBranch(found, output, nil, sdkgo.Receipt{})
}

func decodeAdAccount(body []byte) (AdAccount, error) {
	var wire struct {
		ID   json.Number `json:"id"`
		Name string      `json:"name"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&wire); err != nil || !isDecimalID(wire.ID.String()) || strings.TrimSpace(wire.Name) == "" {
		return AdAccount{}, fmt.Errorf("LinkedIn account JSON invalid")
	}
	return AdAccount{ID: wire.ID.String(), Name: wire.Name}, nil
}
func decodeCampaignGroup(body []byte) (CampaignGroup, error) {
	entry, err := decodeNamedStatus(body)
	if err != nil || !isCampaignGroupStatus(entry.Status) {
		return CampaignGroup{}, fmt.Errorf("LinkedIn campaign group JSON invalid")
	}
	return CampaignGroup{ID: entry.ID, Name: entry.Name, Status: entry.Status}, nil
}
func decodeCampaign(body []byte) (Campaign, error) {
	entry, err := decodeNamedStatus(body)
	if err != nil || !isCampaignStatus(entry.Status) {
		return Campaign{}, fmt.Errorf("LinkedIn campaign JSON invalid")
	}
	return Campaign{ID: entry.ID, Name: entry.Name, Status: entry.Status}, nil
}
func decodeNamedStatus(body []byte) (namedStatus, error) {
	var wire struct {
		ID     json.Number `json:"id"`
		Name   string      `json:"name"`
		Status string      `json:"status"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&wire); err != nil || !isDecimalID(wire.ID.String()) || strings.TrimSpace(wire.Name) == "" {
		return namedStatus{}, fmt.Errorf("LinkedIn named resource JSON invalid")
	}
	return namedStatus{ID: wire.ID.String(), Name: wire.Name, Status: wire.Status}, nil
}
func decodeCreative(body []byte) (Creative, error) {
	var creative Creative
	if err := json.Unmarshal(body, &creative); err != nil || !isSponsoredCreativeURN(creative.ID) || !strings.HasPrefix(creative.Campaign, "urn:li:sponsoredCampaign:") || creative.IntendedStatus == "" {
		return Creative{}, fmt.Errorf("LinkedIn creative JSON invalid")
	}
	return creative, nil
}
func isSponsoredCreativeURN(value string) bool {
	const prefix = "urn:li:sponsoredCreative:"
	return strings.HasPrefix(value, prefix) && isDecimalID(strings.TrimPrefix(value, prefix))
}
