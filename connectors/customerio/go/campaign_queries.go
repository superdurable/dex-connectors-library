// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerio

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CampaignSummary contains the stable public identity and status of one automation.
type CampaignSummary struct {
	// ID is the Customer.io campaign ID.
	ID int64 `json:"id"`
	// Name is the automation name.
	Name string `json:"name"`
	// Active indicates that the automation is enabled.
	Active bool `json:"active"`
	// State is the provider's automation lifecycle state.
	State string `json:"state"`
}

// CampaignsPage contains all automations returned by the App API.
type CampaignsPage struct {
	// Campaigns are the returned automations in provider order.
	Campaigns []CampaignSummary `json:"campaigns"`
}

// GetCampaignInput identifies an automation by its positive ID.
type GetCampaignInput struct {
	// CampaignID is the integer shown in the automation URL.
	CampaignID int64 `json:"campaignId"`
}

// ListCampaignsInput requests the workspace's automations.
type ListCampaignsInput struct{}

// GetCampaignMetricsInput selects a UTC metrics window and increment.
type GetCampaignMetricsInput struct {
	// CampaignID is the positive automation ID.
	CampaignID int64 `json:"campaignId"`
	// StartUnix is the inclusive UTC start time in Unix seconds.
	StartUnix int64 `json:"startUnix"`
	// EndUnix is the UTC end time in Unix seconds.
	EndUnix int64 `json:"endUnix"`
	// Resolution is hours, days, weeks, or months.
	Resolution string `json:"resolution"`
}

// CampaignMetrics contains the provider's numeric series for one campaign.
type CampaignMetrics struct {
	// CampaignID identifies the requested automation.
	CampaignID int64 `json:"campaignId"`
	// Series maps provider metric names to counts in the requested increments.
	Series map[string][]int64 `json:"series"`
}

// GetCampaignOperation reads one automation through the App API.
type GetCampaignOperation struct{ client *Client }

// ListCampaignsOperation reads the workspace's automations.
type ListCampaignsOperation struct{ client *Client }

// GetCampaignMetricsOperation reads a bounded metrics series.
type GetCampaignMetricsOperation struct{ client *Client }

// GetCampaign binds the query to this client.
func (client *Client) GetCampaign() GetCampaignOperation { return GetCampaignOperation{client} }

// ListCampaigns binds the query to this client.
func (client *Client) ListCampaigns() ListCampaignsOperation { return ListCampaignsOperation{client} }

// GetCampaignMetrics binds the query to this client.
func (client *Client) GetCampaignMetrics() GetCampaignMetricsOperation {
	return GetCampaignMetricsOperation{client}
}

// Definition returns the generated GetCampaign contract.
func (GetCampaignOperation) Definition() sdkgo.QueryDefinition { return GetCampaignDefinition }

// Definition returns the generated ListCampaigns contract.
func (ListCampaignsOperation) Definition() sdkgo.QueryDefinition { return ListCampaignsDefinition }

// Definition returns the generated GetCampaignMetrics contract.
func (GetCampaignMetricsOperation) Definition() sdkgo.QueryDefinition {
	return GetCampaignMetricsDefinition
}

// Invoke reads one campaign and distinguishes missing campaigns from rejection.
func (operation GetCampaignOperation) Invoke(call sdkgo.Call, input GetCampaignInput) sdkgo.QueryAttempt[CampaignSummary] {
	return runCampaignQuery(operation.client, call, "/v1/campaigns/"+strconv.FormatInt(input.CampaignID, 10),
		"getCampaign", input.CampaignID > 0, GetCampaignBranchFound, GetCampaignBranchNotFound,
		GetCampaignBranchProviderRejected, GetCampaignBranchInvalidResponse, GetCampaignBranchDefect,
		decodeCampaign)
}

// Invoke reads all campaigns exposed to the App API credential.
func (operation ListCampaignsOperation) Invoke(call sdkgo.Call, _ ListCampaignsInput) sdkgo.QueryAttempt[CampaignsPage] {
	return runCampaignQuery(operation.client, call, "/v1/campaigns", "listCampaigns", true,
		ListCampaignsBranchListed, "", ListCampaignsBranchProviderRejected,
		ListCampaignsBranchInvalidResponse, ListCampaignsBranchDefect, decodeCampaigns)
}

// Invoke reads version-two UTC campaign metrics for a bounded window.
func (operation GetCampaignMetricsOperation) Invoke(call sdkgo.Call, input GetCampaignMetricsInput) sdkgo.QueryAttempt[CampaignMetrics] {
	isResolutionValid := input.Resolution == "hours" || input.Resolution == "days" || input.Resolution == "weeks" || input.Resolution == "months"
	isWindowValid := input.StartUnix > 0 && input.EndUnix > input.StartUnix && input.EndUnix-input.StartUnix <= 10*366*24*60*60
	parameters := url.Values{
		"version": {"2"}, "start": {strconv.FormatInt(input.StartUnix, 10)},
		"end": {strconv.FormatInt(input.EndUnix, 10)}, "res": {input.Resolution},
	}
	decode := func(body []byte) (CampaignMetrics, error) {
		return decodeCampaignMetrics(body, input.CampaignID)
	}
	return runCampaignQuery(operation.client, call, "/v1/campaigns/"+strconv.FormatInt(input.CampaignID, 10)+"/metrics?"+parameters.Encode(),
		"getCampaignMetrics", input.CampaignID > 0 && isWindowValid && isResolutionValid,
		GetCampaignMetricsBranchFound, GetCampaignMetricsBranchNotFound,
		GetCampaignMetricsBranchProviderRejected, GetCampaignMetricsBranchInvalidResponse,
		GetCampaignMetricsBranchDefect, decode)
}

func runCampaignQuery[T any](client *Client, call sdkgo.Call, path string, operation string, isInputValid bool, happyBranch sdkgo.BranchID, notFoundBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID, invalidBranch sdkgo.BranchID, defectBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	classifier := campaignQueryClassifier[T]{operation: operation}
	if !isInputValid {
		return classifier.branch(defectBranch, sdkgo.FailureValidation, "invalid campaign ID or metrics window")
	}
	response := client.getPath(call, path)
	if errors.Is(response.err, errCredential) {
		return classifier.branch(defectBranch, sdkgo.FailureAuthentication, "Customer.io credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](classifier.failure(sdkgo.FailureTransport, "Customer.io request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](classifier.failure(sdkgo.FailureRateLimit, "Customer.io rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](classifier.failure(sdkgo.FailureAvailability, "Customer.io is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound && notFoundBranch != "" {
		return classifier.branch(notFoundBranch, sdkgo.FailureNotFound, "Customer.io campaign was not found")
	}
	if response.status != http.StatusOK {
		return classifier.branch(rejectedBranch, sdkgo.FailureProviderRejection, "Customer.io rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return classifier.branch(invalidBranch, sdkgo.FailureResponseTooLarge, "Customer.io response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](classifier.failure(sdkgo.FailureTransport, "Customer.io response was interrupted"), 0)
	}
	output, err := decode(response.body)
	if err != nil {
		return classifier.branch(invalidBranch, sdkgo.FailureProtocol, "Customer.io returned invalid campaign JSON")
	}
	return sdkgo.NewQueryBranch(happyBranch, output, nil, sdkgo.Receipt{})
}

type campaignQueryClassifier[T any] struct{ operation string }

func (classifier campaignQueryClassifier[T]) failure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "customerio", Operation: classifier.operation, Message: message}
}

func (classifier campaignQueryClassifier[T]) branch(id sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var empty T
	failure := classifier.failure(kind, message)
	return sdkgo.NewQueryBranch(id, empty, &failure, sdkgo.Receipt{})
}

func decodeCampaign(body []byte) (CampaignSummary, error) {
	var envelope struct {
		Campaign CampaignSummary `json:"campaign"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return CampaignSummary{}, err
	}
	if envelope.Campaign.ID <= 0 || strings.TrimSpace(envelope.Campaign.Name) == "" {
		return CampaignSummary{}, errors.New("campaign identity missing")
	}
	return envelope.Campaign, nil
}

func decodeCampaigns(body []byte) (CampaignsPage, error) {
	var envelope struct {
		Campaigns []CampaignSummary `json:"campaigns"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Campaigns == nil {
		return CampaignsPage{}, errors.New("campaign list missing")
	}
	for _, campaign := range envelope.Campaigns {
		if campaign.ID <= 0 || strings.TrimSpace(campaign.Name) == "" {
			return CampaignsPage{}, errors.New("campaign identity missing")
		}
	}
	return CampaignsPage{Campaigns: envelope.Campaigns}, nil
}

func decodeCampaignMetrics(body []byte, campaignID int64) (CampaignMetrics, error) {
	var envelope struct {
		Metric struct {
			Series map[string][]int64 `json:"series"`
		} `json:"metric"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Metric.Series == nil {
		return CampaignMetrics{}, errors.New("campaign metrics missing")
	}
	for metric, counts := range envelope.Metric.Series {
		if metric == "" || counts == nil {
			return CampaignMetrics{}, errors.New("campaign metric missing")
		}
		for _, count := range counts {
			if count < 0 {
				return CampaignMetrics{}, errors.New("negative campaign metric")
			}
		}
	}
	return CampaignMetrics{CampaignID: campaignID, Series: envelope.Metric.Series}, nil
}
