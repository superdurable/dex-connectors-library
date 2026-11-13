// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rainbond

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListTeamsInput selects a one-based team page; zero page and size use 1 and 20.
type ListTeamsInput struct {
	// Page is the one-based page number.
	Page int `json:"page"`
	// PageSize is the number of teams to request, from 1 to 100.
	PageSize int `json:"pageSize"`
}

// Team contains only non-personal Rainbond team metadata.
type Team struct {
	// ID is the Rainbond tenant_id.
	ID string `json:"id"`
	// Name is the Rainbond tenant_name.
	Name string `json:"name"`
	// Active reports whether the team is active.
	Active bool `json:"active"`
}

// ListTeamsOutput contains one team page and the total count.
type ListTeamsOutput struct {
	// Teams are the returned teams in provider order.
	Teams []Team `json:"teams"`
	// Total is the total number of accessible teams.
	Total int `json:"total"`
}

// ListTeamsOperation reads the Rainbond team list.
type ListTeamsOperation struct{ client *Client }

// Definition returns the generated team-list contract.
func (ListTeamsOperation) Definition() sdkgo.QueryDefinition { return ListTeamsDefinition }

// Invoke lists one page of teams and classifies provider and transport outcomes.
func (operation ListTeamsOperation) Invoke(call sdkgo.Call, input ListTeamsInput) sdkgo.QueryAttempt[ListTeamsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return teamCatalogBranch(ListTeamsBranchDefect, sdkgo.FailureAuthentication, "console access token is unavailable or invalid")
	}
	page, pageSize := input.Page, input.PageSize
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100 {
		return teamCatalogBranch(ListTeamsBranchDefect, sdkgo.FailureValidation, "page or pageSize is outside the supported range")
	}
	values := url.Values{}
	values.Set("page", strconv.Itoa(page))
	values.Set("page_size", strconv.Itoa(pageSize))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/openapi/v1/teams?"+values.Encode(), nil)
	if err != nil {
		return teamCatalogBranch(ListTeamsBranchDefect, sdkgo.FailureValidation, "team-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return teamCatalogRetry(sdkgo.FailureAvailability, "Rainbond request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return teamCatalogRetry(sdkgo.FailureRateLimit, "Rainbond rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return teamCatalogRetry(sdkgo.FailureAvailability, "Rainbond is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return teamCatalogBranch(ListTeamsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Rainbond response exceeded the configured limit")
	}
	if err != nil {
		return teamCatalogRetry(sdkgo.FailureAvailability, "Rainbond response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return teamCatalogBranch(ListTeamsBranchProviderRejected, kind, "Rainbond rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Total   *int `json:"total"`
		Tenants []struct {
			ID     string `json:"tenant_id"`
			Name   string `json:"tenant_name"`
			Active *bool  `json:"is_active"`
		} `json:"tenants"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Total == nil || *wire.Total < 0 || wire.Tenants == nil || len(wire.Tenants) > pageSize {
		return teamCatalogBranch(ListTeamsBranchInvalidResponse, sdkgo.FailureProtocol, "Rainbond returned invalid team-list data")
	}
	teams := make([]Team, 0, len(wire.Tenants))
	for _, tenant := range wire.Tenants {
		if tenant.ID == "" || len(tenant.ID) > 256 || strings.TrimSpace(tenant.ID) != tenant.ID || tenant.Name == "" || len(tenant.Name) > 512 {
			return teamCatalogBranch(ListTeamsBranchInvalidResponse, sdkgo.FailureProtocol, "Rainbond returned an invalid team")
		}
		active := false
		if tenant.Active != nil {
			active = *tenant.Active
		}
		teams = append(teams, Team{ID: tenant.ID, Name: tenant.Name, Active: active})
	}
	return sdkgo.NewQueryBranch(ListTeamsBranchListed, ListTeamsOutput{Teams: teams, Total: *wire.Total}, nil, sdkgo.Receipt{})
}

func isValidAPIKey(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func teamCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListTeamsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "rainbond", Operation: "listTeams", Message: message}
	return sdkgo.NewQueryBranch(branch, ListTeamsOutput{}, &failure, sdkgo.Receipt{})
}
func teamCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListTeamsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "rainbond", Operation: "listTeams", Message: message}
	return sdkgo.NewQueryRetry[ListTeamsOutput](failure, delay)
}
