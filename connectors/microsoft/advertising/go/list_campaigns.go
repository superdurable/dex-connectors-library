// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const listCampaignsPath = "/CampaignManagement/v13/Campaigns/QueryByAccountId"
const allCampaignTypes = "App Audience Hotel ObjectiveBased PerformanceMax Search Shopping"

var advertisingIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// ListCampaignsInput identifies the Microsoft Advertising account to query.
type ListCampaignsInput struct {
	// AccountID is the numeric aid in the signed-in Campaigns URL.
	AccountID string `json:"accountId"`
}

// CampaignSummary contains public campaign fields selected for a Flow.
type CampaignSummary struct {
	// ID is the stable Microsoft Advertising campaign ID.
	ID string `json:"id"`
	// Name is the campaign's display name.
	Name string `json:"name"`
	// CampaignType is the provider campaign type.
	CampaignType string `json:"campaignType"`
	// Status is the campaign's provider status.
	Status string `json:"status"`
}

// CampaignsPage is the bounded campaign list returned for one account.
type CampaignsPage struct {
	// Campaigns contains the returned campaigns, which may be empty.
	Campaigns []CampaignSummary `json:"campaigns"`
}

// ListCampaignsOperation reads campaigns without changing them.
type ListCampaignsOperation struct{ client *Client }

// ListCampaigns binds the Query to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Invoke classifies credential, transport, HTTP, and campaign response outcomes.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, input ListCampaignsInput) sdkgo.QueryAttempt[CampaignsPage] {
	if _, err := strconv.ParseInt(input.AccountID, 10, 64); err != nil || !advertisingIDPattern.MatchString(input.AccountID) {
		return campaignsBranch(ListCampaignsBranchDefect, sdkgo.FailureValidation, "Microsoft Advertising account ID is invalid")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return campaignsBranch(ListCampaignsBranchDefect, sdkgo.FailureAuthentication, "Microsoft Advertising connection needs authorization")
		}
		return campaignsRetry(sdkgo.FailureTransport, "Microsoft Advertising credential refresh failed", 0)
	}
	accessToken := credentials.AccessToken.Reveal()
	developerToken := credentials.DeveloperToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) || !providerhttp.IsHeaderSafeCredential(developerToken) {
		return campaignsBranch(ListCampaignsBranchDefect, sdkgo.FailureAuthentication, "Microsoft Advertising connection credentials are unavailable")
	}
	payload, err := json.Marshal(struct {
		AccountID    string `json:"AccountId"`
		CampaignType string `json:"CampaignType"`
	}{input.AccountID, allCampaignTypes})
	if err != nil {
		return campaignsBranch(ListCampaignsBranchDefect, sdkgo.FailureValidation, "Microsoft Advertising request could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+listCampaignsPath, bytes.NewReader(payload))
	if err != nil {
		return campaignsBranch(ListCampaignsBranchDefect, sdkgo.FailureValidation, "Microsoft Advertising request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("DeveloperToken", developerToken)
	request.Header.Set("CustomerId", operation.client.customerID)
	request.Header.Set("CustomerAccountId", input.AccountID)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return campaignsRetry(sdkgo.FailureTransport, "Microsoft Advertising campaign request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) || bytes.Contains(body, []byte(developerToken)) {
		return campaignsBranch(ListCampaignsBranchDefect, sdkgo.FailureAuthentication, "Microsoft Advertising response contained a credential")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return campaignsRetry(sdkgo.FailureRateLimit, "Microsoft Advertising rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return campaignsRetry(sdkgo.FailureAvailability, "Microsoft Advertising is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return campaignsBranch(ListCampaignsBranchProviderRejected, sdkgo.FailureProviderRejection, "Microsoft Advertising rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return campaignsBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Microsoft Advertising campaign response exceeded the configured limit")
	}
	if readErr != nil {
		return campaignsRetry(sdkgo.FailureTransport, "Microsoft Advertising campaign response was interrupted", 0)
	}
	page, err := decodeCampaigns(body)
	if err != nil {
		return campaignsBranch(ListCampaignsBranchInvalidResponse, sdkgo.FailureProtocol, "Microsoft Advertising returned invalid campaign JSON")
	}
	return sdkgo.NewQueryBranch(ListCampaignsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeCampaigns(body []byte) (CampaignsPage, error) {
	var response struct {
		Campaigns *[]struct {
			ID           string `json:"Id"`
			Name         string `json:"Name"`
			CampaignType string `json:"CampaignType"`
			Status       string `json:"Status"`
		} `json:"Campaigns"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return CampaignsPage{}, err
	}
	if response.Campaigns == nil {
		return CampaignsPage{}, errors.New("campaign list is missing")
	}
	page := CampaignsPage{Campaigns: make([]CampaignSummary, 0, len(*response.Campaigns))}
	for _, campaign := range *response.Campaigns {
		if _, err := strconv.ParseInt(campaign.ID, 10, 64); err != nil || !advertisingIDPattern.MatchString(campaign.ID) || campaign.Name == "" || campaign.CampaignType == "" || campaign.Status == "" {
			return CampaignsPage{}, errors.New("campaign list has an incomplete record")
		}
		page.Campaigns = append(page.Campaigns, CampaignSummary{campaign.ID, campaign.Name, campaign.CampaignType, campaign.Status})
	}
	return page, nil
}

func campaignsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "microsoft-advertising", Operation: "listCampaigns", Message: message}
}

func campaignsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CampaignsPage] {
	failure := campaignsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CampaignsPage{}, &failure, sdkgo.Receipt{})
}

func campaignsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[CampaignsPage] {
	return sdkgo.NewQueryRetry[CampaignsPage](campaignsFailure(kind, message), delay)
}
