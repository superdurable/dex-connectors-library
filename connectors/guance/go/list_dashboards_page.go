// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package guance

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ListDashboardsPageInput selects one provider page; zeros use page 1 and size 10.
type ListDashboardsPageInput struct {
	// PageIndex is the one-based page number; zero means the first page.
	PageIndex int `json:"pageIndex"`
	// PageSize is the number of dashboards per page, from 1 to 100; zero means 10.
	PageSize int `json:"pageSize"`
}

// DashboardMetadata identifies a dashboard without user details or chart content.
type DashboardMetadata struct {
	// UUID is the stable dashboard identifier.
	UUID string `json:"uuid"`
	// Name is the dashboard display name.
	Name string `json:"name"`
}

// ListDashboardsPageOutput contains one page of safe dashboard metadata.
type ListDashboardsPageOutput struct {
	// TotalCount is the total number of matching dashboards.
	TotalCount int `json:"totalCount"`
	// PageIndex is the one-based page number returned by Guance.
	PageIndex int `json:"pageIndex"`
	// PageSize is the page size returned by Guance.
	PageSize int `json:"pageSize"`
	// Dashboards are the identifiers and names in this page.
	Dashboards []DashboardMetadata `json:"dashboards"`
}

// ListDashboardsPageOperation reads dashboard metadata without modifying provider state.
type ListDashboardsPageOperation struct{ client *Client }

// Definition returns the generated dashboard-list contract.
func (ListDashboardsPageOperation) Definition() sdkgo.QueryDefinition {
	return ListDashboardsPageDefinition
}

// Invoke reads one page and excludes provider user details from durable output.
func (operation ListDashboardsPageOperation) Invoke(call sdkgo.Call, input ListDashboardsPageInput) sdkgo.QueryAttempt[ListDashboardsPageOutput] {
	if input.PageIndex < 0 || input.PageSize < 0 || input.PageSize > 100 {
		return dashboardBranch(ListDashboardsPageBranchDefect, sdkgo.FailureValidation, "Guance pagination is invalid")
	}
	pageIndex, pageSize := input.PageIndex, input.PageSize
	if pageIndex == 0 {
		pageIndex = 1
	}
	if pageSize == 0 {
		pageSize = 10
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return dashboardBranch(ListDashboardsPageBranchDefect, sdkgo.FailureAuthentication, "Guance API Key is unavailable or invalid")
	}
	query := url.Values{"pageIndex": {strconv.Itoa(pageIndex)}, "pageSize": {strconv.Itoa(pageSize)}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, dashboardsEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return dashboardBranch(ListDashboardsPageBranchDefect, sdkgo.FailureValidation, "Guance dashboard-list request is invalid")
	}
	request.Header.Set("DF-API-KEY", credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return dashboardRetry(sdkgo.FailureAvailability, "Guance dashboard-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return dashboardRetry(sdkgo.FailureRateLimit, "Guance rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return dashboardRetry(sdkgo.FailureAvailability, "Guance dashboard list is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyDashboardHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return dashboardBranch(ListDashboardsPageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Guance response exceeded the configured limit")
	}
	if err != nil {
		return dashboardRetry(sdkgo.FailureAvailability, "Guance response was interrupted", 0)
	}
	var wire struct {
		Code    *int  `json:"code"`
		Success *bool `json:"success"`
		Content []struct {
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"content"`
		PageInfo *struct {
			Count      int `json:"count"`
			PageIndex  int `json:"pageIndex"`
			PageSize   int `json:"pageSize"`
			TotalCount int `json:"totalCount"`
		} `json:"pageInfo"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil || wire.Success == nil {
		return dashboardBranch(ListDashboardsPageBranchInvalidResponse, sdkgo.FailureProtocol, "Guance returned invalid dashboard metadata")
	}
	if !*wire.Success || *wire.Code != 200 {
		return dashboardBranch(ListDashboardsPageBranchProviderRejected, sdkgo.FailureProviderRejection, "Guance rejected the dashboard-list request")
	}
	if wire.Content == nil || wire.PageInfo == nil || wire.PageInfo.Count != len(wire.Content) || wire.PageInfo.Count < 0 || wire.PageInfo.PageIndex != pageIndex || wire.PageInfo.PageSize < 1 || wire.PageInfo.PageSize > 100 || wire.PageInfo.TotalCount < wire.PageInfo.Count {
		return dashboardBranch(ListDashboardsPageBranchInvalidResponse, sdkgo.FailureProtocol, "Guance returned invalid dashboard pagination")
	}
	output := ListDashboardsPageOutput{TotalCount: wire.PageInfo.TotalCount, PageIndex: wire.PageInfo.PageIndex, PageSize: wire.PageInfo.PageSize, Dashboards: make([]DashboardMetadata, 0, len(wire.Content))}
	for _, dashboard := range wire.Content {
		if dashboard.UUID == "" || dashboard.Name == "" {
			return dashboardBranch(ListDashboardsPageBranchInvalidResponse, sdkgo.FailureProtocol, "Guance returned incomplete dashboard metadata")
		}
		output.Dashboards = append(output.Dashboards, DashboardMetadata{UUID: dashboard.UUID, Name: dashboard.Name})
	}
	return sdkgo.NewQueryBranch(ListDashboardsPageBranchListed, output, nil, sdkgo.Receipt{})
}

func classifyDashboardHTTPStatus(status int) sdkgo.QueryAttempt[ListDashboardsPageOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return dashboardBranch(ListDashboardsPageBranchProviderRejected, kind, "Guance rejected the dashboard-list request with HTTP "+strconv.Itoa(status))
}
func dashboardBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListDashboardsPageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "guance", Operation: "listDashboardsPage", Message: message}
	return sdkgo.NewQueryBranch(branch, ListDashboardsPageOutput{}, &failure, sdkgo.Receipt{})
}
func dashboardRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListDashboardsPageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "guance", Operation: "listDashboardsPage", Message: message}
	return sdkgo.NewQueryRetry[ListDashboardsPageOutput](failure, delay)
}
