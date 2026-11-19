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
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListCampaignGroupsInput selects campaign groups in one account and status.
type ListCampaignGroupsInput struct {
	AccountID string `json:"accountId"`
	Status    string `json:"status"`
	PageSize  int    `json:"pageSize,omitempty"`
	PageToken string `json:"pageToken,omitempty"`
}

// CampaignGroup identifies a LinkedIn campaign group and its current status.
type CampaignGroup struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// CampaignGroupsPage contains one matching group page and continuation cursor.
type CampaignGroupsPage struct {
	Groups        []CampaignGroup `json:"groups"`
	NextPageToken string          `json:"nextPageToken"`
}

// ListCampaignGroupsOperation reads campaign groups by status.
type ListCampaignGroupsOperation struct{ client *Client }

// ListCampaignGroups binds the campaign-group Query to this client.
func (client *Client) ListCampaignGroups() ListCampaignGroupsOperation {
	return ListCampaignGroupsOperation{client}
}

// Definition returns the manifest-generated campaign-group contract.
func (ListCampaignGroupsOperation) Definition() sdkgo.QueryDefinition {
	return ListCampaignGroupsDefinition
}

// Invoke reads one bounded campaign-group page.
func (operation ListCampaignGroupsOperation) Invoke(call sdkgo.Call, input ListCampaignGroupsInput) sdkgo.QueryAttempt[CampaignGroupsPage] {
	if !isDecimalID(input.AccountID) || !isCampaignGroupStatus(input.Status) || !isPageRequestValid(input.PageSize, input.PageToken, 1000) {
		return queryBranch[CampaignGroupsPage](ListCampaignGroupsBranchDefect, sdkgo.FailureValidation, "campaign-group search input is invalid", "listCampaignGroups")
	}
	pageSize := defaultPageSize(input.PageSize)
	path := campaignSearchPath(input.AccountID, "adCampaignGroups", input.Status, pageSize, input.PageToken)
	return readLinkedInPage(operation.client.get(call, path), pageSize, "listCampaignGroups", ListCampaignGroupsBranchListed, ListCampaignGroupsBranchProviderRejected, ListCampaignGroupsBranchInvalidResponse, ListCampaignGroupsBranchDefect, decodeCampaignGroupsPage)
}

// ListCampaignsInput selects campaigns in one account and status.
type ListCampaignsInput struct {
	AccountID string `json:"accountId"`
	Status    string `json:"status"`
	PageSize  int    `json:"pageSize,omitempty"`
	PageToken string `json:"pageToken,omitempty"`
}

// Campaign identifies a LinkedIn campaign and its current status.
type Campaign struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// CampaignsPage contains one matching campaign page and continuation cursor.
type CampaignsPage struct {
	Campaigns     []Campaign `json:"campaigns"`
	NextPageToken string     `json:"nextPageToken"`
}

// ListCampaignsOperation reads campaigns by status.
type ListCampaignsOperation struct{ client *Client }

// ListCampaigns binds the campaign Query to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// Definition returns the manifest-generated campaign contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Invoke reads one bounded campaign page.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListCampaignsInput) sdkgo.QueryAttempt[CampaignsPage] {
	if !isDecimalID(input.AccountID) || !isCampaignStatus(input.Status) || !isPageRequestValid(input.PageSize, input.PageToken, 1000) {
		return queryBranch[CampaignsPage](ListCampaignsBranchDefect, sdkgo.FailureValidation, "campaign search input is invalid", "listCampaigns")
	}
	pageSize := defaultPageSize(input.PageSize)
	path := campaignSearchPath(input.AccountID, "adCampaigns", input.Status, pageSize, input.PageToken)
	return readLinkedInPage(operation.client.get(call, path), pageSize, "listCampaigns", ListCampaignsBranchListed, ListCampaignsBranchProviderRejected, ListCampaignsBranchInvalidResponse, ListCampaignsBranchDefect, decodeCampaignsPage)
}

// ListCreativesInput selects creatives for one campaign in an account.
type ListCreativesInput struct {
	AccountID  string `json:"accountId"`
	CampaignID string `json:"campaignId"`
	PageSize   int    `json:"pageSize,omitempty"`
	PageToken  string `json:"pageToken,omitempty"`
}

// Creative identifies a LinkedIn creative and its campaign and status.
type Creative struct {
	ID             string `json:"id"`
	Campaign       string `json:"campaign"`
	IntendedStatus string `json:"intendedStatus"`
}

// CreativesPage contains one matching creative page and continuation cursor.
type CreativesPage struct {
	Creatives     []Creative `json:"creatives"`
	NextPageToken string     `json:"nextPageToken"`
}

// ListCreativesOperation reads creatives for one campaign.
type ListCreativesOperation struct{ client *Client }

// ListCreatives binds the creative Query to this client.
func (client *Client) ListCreatives() ListCreativesOperation { return ListCreativesOperation{client} }

// Definition returns the manifest-generated creative contract.
func (ListCreativesOperation) Definition() sdkgo.QueryDefinition { return ListCreativesDefinition }

// Invoke reads one bounded creative page.
func (operation ListCreativesOperation) Invoke(call sdkgo.Call, input ListCreativesInput) sdkgo.QueryAttempt[CreativesPage] {
	if !isDecimalID(input.AccountID) || !isDecimalID(input.CampaignID) || !isPageRequestValid(input.PageSize, input.PageToken, 100) {
		return queryBranch[CreativesPage](ListCreativesBranchDefect, sdkgo.FailureValidation, "creative search input is invalid", "listCreatives")
	}
	pageSize := defaultPageSize(input.PageSize)
	query := url.Values{"q": {"criteria"}, "pageSize": {strconv.Itoa(pageSize)}, "campaigns": {"List(urn:li:sponsoredCampaign:" + input.CampaignID + ")"}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	path := "/rest/adAccounts/" + input.AccountID + "/creatives?" + query.Encode()
	return readLinkedInPage(operation.client.getFinder(call, path), pageSize, "listCreatives", ListCreativesBranchListed, ListCreativesBranchProviderRejected, ListCreativesBranchInvalidResponse, ListCreativesBranchDefect, decodeCreativesPage)
}

func isDecimalID(value string) bool {
	if value == "" || len(value) > 16 || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func isCampaignGroupStatus(status string) bool {
	switch status {
	case "ACTIVE", "ARCHIVED", "CANCELED", "DRAFT", "PAUSED", "PENDING_DELETION", "REMOVED":
		return true
	}
	return false
}

func isCampaignStatus(status string) bool {
	return isCampaignGroupStatus(status) || status == "COMPLETED"
}

func isPageRequestValid(pageSize int, pageToken string, maximum int) bool {
	return pageSize >= 0 && pageSize <= maximum && isSafePageToken(pageToken)
}

func defaultPageSize(pageSize int) int {
	if pageSize == 0 {
		return 100
	}
	return pageSize
}

func campaignSearchPath(accountID, collection, status string, pageSize int, pageToken string) string {
	query := url.Values{"q": {"search"}, "search": {"(status:(values:List(" + status + ")))"}, "pageSize": {strconv.Itoa(pageSize)}}
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}
	return "/rest/adAccounts/" + accountID + "/" + collection + "?" + query.Encode()
}

func readLinkedInPage[Output any](response providerResponse, pageSize int, operation string, listed, rejected, invalid, defect sdkgo.BranchID, decode func([]byte, int) (Output, error)) sdkgo.QueryAttempt[Output] {
	if errors.Is(response.err, errCredential) {
		return queryBranch[Output](defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid", operation)
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
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch[Output](invalid, sdkgo.FailureResponseTooLarge, "LinkedIn Ads response exceeded the configured limit", operation)
	}
	if response.err != nil {
		return queryRetry[Output](sdkgo.FailureTransport, "LinkedIn Ads response was interrupted", 0, operation)
	}
	if response.status != http.StatusOK {
		return queryBranch[Output](rejected, sdkgo.FailureProviderRejection, "LinkedIn Ads rejected HTTP "+strconv.Itoa(response.status), operation)
	}
	output, err := decode(response.body, pageSize)
	if err != nil {
		return queryBranch[Output](invalid, sdkgo.FailureProtocol, "LinkedIn Ads returned invalid resource JSON", operation)
	}
	return sdkgo.NewQueryBranch(listed, output, nil, sdkgo.Receipt{})
}

func queryBranch[Output any](branch sdkgo.BranchID, kind sdkgo.FailureKind, message, operation string) sdkgo.QueryAttempt[Output] {
	failure := sdkgo.Failure{Kind: kind, Provider: "linkedin-ads", Operation: operation, Message: message}
	var zero Output
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func queryRetry[Output any](kind sdkgo.FailureKind, message string, delay time.Duration, operation string) sdkgo.QueryAttempt[Output] {
	return sdkgo.NewQueryRetry[Output](sdkgo.Failure{Kind: kind, Provider: "linkedin-ads", Operation: operation, Message: message}, delay)
}

func decodeCampaignGroupsPage(body []byte, pageSize int) (CampaignGroupsPage, error) {
	entries, token, err := decodeNamedStatuses(body, pageSize)
	if err != nil {
		return CampaignGroupsPage{}, err
	}
	groups := make([]CampaignGroup, 0, len(entries))
	for _, entry := range entries {
		if !isCampaignGroupStatus(entry.Status) {
			return CampaignGroupsPage{}, fmt.Errorf("invalid campaign group status")
		}
		groups = append(groups, CampaignGroup{ID: entry.ID, Name: entry.Name, Status: entry.Status})
	}
	return CampaignGroupsPage{Groups: groups, NextPageToken: token}, nil
}

func decodeCampaignsPage(body []byte, pageSize int) (CampaignsPage, error) {
	entries, token, err := decodeNamedStatuses(body, pageSize)
	if err != nil {
		return CampaignsPage{}, err
	}
	campaigns := make([]Campaign, 0, len(entries))
	for _, entry := range entries {
		if !isCampaignStatus(entry.Status) {
			return CampaignsPage{}, fmt.Errorf("invalid campaign status")
		}
		campaigns = append(campaigns, Campaign{ID: entry.ID, Name: entry.Name, Status: entry.Status})
	}
	return CampaignsPage{Campaigns: campaigns, NextPageToken: token}, nil
}

type namedStatus struct {
	ID     string
	Name   string
	Status string
}

func decodeNamedStatuses(body []byte, pageSize int) ([]namedStatus, string, error) {
	var payload struct {
		Elements []*struct {
			ID     json.Number `json:"id"`
			Name   string      `json:"name"`
			Status string      `json:"status"`
		} `json:"elements"`
		Metadata struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"metadata"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, "", err
	}
	if payload.Elements == nil || len(payload.Elements) > pageSize || !isSafePageToken(payload.Metadata.NextPageToken) {
		return nil, "", fmt.Errorf("invalid resource page")
	}
	entries := make([]namedStatus, 0, len(payload.Elements))
	seen := make(map[string]bool, len(payload.Elements))
	for _, entry := range payload.Elements {
		if entry == nil || !isDecimalID(entry.ID.String()) || strings.TrimSpace(entry.Name) == "" || seen[entry.ID.String()] {
			return nil, "", fmt.Errorf("invalid resource")
		}
		seen[entry.ID.String()] = true
		entries = append(entries, namedStatus{ID: entry.ID.String(), Name: entry.Name, Status: entry.Status})
	}
	return entries, payload.Metadata.NextPageToken, nil
}

func decodeCreativesPage(body []byte, pageSize int) (CreativesPage, error) {
	var payload struct {
		Elements []*Creative `json:"elements"`
		Metadata struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CreativesPage{}, err
	}
	if payload.Elements == nil || len(payload.Elements) > pageSize || !isSafePageToken(payload.Metadata.NextPageToken) {
		return CreativesPage{}, fmt.Errorf("invalid creative page")
	}
	creatives := make([]Creative, 0, len(payload.Elements))
	seen := make(map[string]bool, len(payload.Elements))
	for _, entry := range payload.Elements {
		if entry == nil || !strings.HasPrefix(entry.ID, "urn:li:sponsoredCreative:") || !strings.HasPrefix(entry.Campaign, "urn:li:sponsoredCampaign:") || entry.IntendedStatus == "" || seen[entry.ID] {
			return CreativesPage{}, fmt.Errorf("invalid creative")
		}
		seen[entry.ID] = true
		creatives = append(creatives, *entry)
	}
	return CreativesPage{Creatives: creatives, NextPageToken: payload.Metadata.NextPageToken}, nil
}
