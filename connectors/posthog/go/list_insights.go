// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package posthog

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListInsightsInput selects a project and bounded offset page of saved insights.
type ListInsightsInput struct {
	// ProjectID is the positive ID shown in the PostHog project URL or settings.
	ProjectID int64 `json:"projectId"`
	// Offset is the zero-based page offset.
	Offset int `json:"offset"`
	// Limit is the maximum count from 1 through 100; zero uses 20.
	Limit int `json:"limit,omitempty"`
}

// InsightMetadata contains only a saved insight's identity and display name.
type InsightMetadata struct {
	// ID is the stable numeric insight ID.
	ID int64 `json:"id"`
	// Name is the untrusted display name.
	Name string `json:"name"`
}

// InsightsPage is one bounded saved-insight page without analysis results.
type InsightsPage struct {
	// Insights contains the selected metadata page.
	Insights []InsightMetadata `json:"insights"`
	// HasMore indicates a provider next page.
	HasMore bool `json:"hasMore"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
}

// ListInsightsOperation reads saved insight identities.
type ListInsightsOperation struct{ client *Client }

// ListInsights binds the read query to this client.
func (client *Client) ListInsights() ListInsightsOperation { return ListInsightsOperation{client} }

// Definition returns the manifest-generated insight-list contract.
func (ListInsightsOperation) Definition() sdkgo.QueryDefinition { return ListInsightsDefinition }

// Invoke reads a bounded page of saved insight IDs and names.
func (operation ListInsightsOperation) Invoke(call sdkgo.Call, input ListInsightsInput) sdkgo.QueryAttempt[InsightsPage] {
	if input.ProjectID <= 0 || input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return insightsFailure(ListInsightsBranchDefect, sdkgo.FailureValidation, "project ID, offset, or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.PersonalAPIKey.Reveal()) || credentials.PersonalAPIKey.Reveal() == "" {
		return insightsFailure(ListInsightsBranchDefect, sdkgo.FailureAuthentication, "PostHog personal API key is unavailable or invalid")
	}
	query := url.Values{"offset": {strconv.Itoa(input.Offset)}, "limit": {strconv.Itoa(input.Limit)}}
	endpoint := operation.client.appURL + "/api/projects/" + strconv.FormatInt(input.ProjectID, 10) + "/insights/?" + query.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return insightsFailure(ListInsightsBranchDefect, sdkgo.FailureLocalDefect, "insight request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.PersonalAPIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return insightsRetry(sdkgo.FailureTransport, "PostHog insight response is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return insightsRetry(sdkgo.FailureRateLimit, "PostHog rate limited insight listing", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return insightsRetry(sdkgo.FailureAvailability, "PostHog insight listing is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return insightsFailure(ListInsightsBranchProviderRejected, posthogRejectionKind(response.StatusCode), "PostHog rejected insight listing with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return insightsFailure(ListInsightsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "PostHog insight page exceeded the limit")
	}
	if err != nil {
		return insightsRetry(sdkgo.FailureTransport, "PostHog insight response was interrupted", 0)
	}
	page, err := decodeInsightsPage(body, input)
	if err != nil {
		return insightsFailure(ListInsightsBranchInvalidResponse, sdkgo.FailureProtocol, "PostHog returned an invalid insight page")
	}
	return sdkgo.NewQueryBranch(ListInsightsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeInsightsPage(body []byte, input ListInsightsInput) (InsightsPage, error) {
	var wire struct {
		Next    *string `json:"next"`
		Results []struct {
			ID   int64   `json:"id"`
			Name *string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return InsightsPage{}, err
	}
	if wire.Results == nil || len(wire.Results) > input.Limit {
		return InsightsPage{}, fmt.Errorf("missing or oversized results")
	}
	page := InsightsPage{Insights: make([]InsightMetadata, 0, len(wire.Results)), HasMore: wire.Next != nil && *wire.Next != "", Offset: input.Offset, Limit: input.Limit}
	for _, insight := range wire.Results {
		if insight.ID <= 0 {
			return InsightsPage{}, fmt.Errorf("insight ID missing")
		}
		name := ""
		if insight.Name != nil {
			name = *insight.Name
		}
		page.Insights = append(page.Insights, InsightMetadata{ID: insight.ID, Name: name})
	}
	return page, nil
}
func insightsFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[InsightsPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "posthog", Operation: "listInsights", Message: message}
	return sdkgo.NewQueryBranch(branch, InsightsPage{}, &failure, sdkgo.Receipt{})
}
func insightsRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[InsightsPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "posthog", Operation: "listInsights", Message: message}
	return sdkgo.NewQueryRetry[InsightsPage](failure, after)
}
