// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ones

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListTeamsInput has no arguments; the API key identifies the accessible teams.
type ListTeamsInput struct{}

// Team contains non-personal team metadata.
type Team struct {
	// ID is the ONES team UUID.
	ID string `json:"id"`
	// Name is the team name.
	Name string `json:"name"`
	// Status is the provider's numeric team status.
	Status int `json:"status"`
}

// ListTeamsOutput contains teams accessible to the API key.
type ListTeamsOutput struct {
	// Teams are the accessible teams in provider order.
	Teams []Team `json:"teams"`
}

// ListTeamsOperation reads the ONES team list.
type ListTeamsOperation struct{ client *Client }

// Definition returns the generated team-list contract.
func (ListTeamsOperation) Definition() sdkgo.QueryDefinition { return ListTeamsDefinition }

// Invoke lists teams and classifies provider and transport outcomes.
func (operation ListTeamsOperation) Invoke(call sdkgo.Call, _ ListTeamsInput) sdkgo.QueryAttempt[ListTeamsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return teamCatalogBranch(ListTeamsBranchDefect, sdkgo.FailureAuthentication, "connection API key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/openapi/v2/account/teams", nil)
	if err != nil {
		return teamCatalogBranch(ListTeamsBranchDefect, sdkgo.FailureValidation, "team-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return teamCatalogRetry(sdkgo.FailureAvailability, "ONES request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return teamCatalogRetry(sdkgo.FailureRateLimit, "ONES rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return teamCatalogRetry(sdkgo.FailureAvailability, "ONES is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return teamCatalogBranch(ListTeamsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ONES response exceeded the configured limit")
	}
	if err != nil {
		return teamCatalogRetry(sdkgo.FailureAvailability, "ONES response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return teamCatalogBranch(ListTeamsBranchProviderRejected, kind, "ONES rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Result    string `json:"result"`
		ErrorCode string `json:"errorCode"`
		Data      struct {
			Teams []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Status *int   `json:"status"`
			} `json:"teams"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return teamCatalogBranch(ListTeamsBranchInvalidResponse, sdkgo.FailureProtocol, "ONES returned invalid team-list data")
	}
	if wire.Result == "FAIL" {
		kind := sdkgo.FailureProviderRejection
		if wire.ErrorCode == "Unauthorized" {
			kind = sdkgo.FailureAuthentication
		}
		if wire.ErrorCode == "Forbidden" {
			kind = sdkgo.FailureAuthorization
		}
		return teamCatalogBranch(ListTeamsBranchProviderRejected, kind, "ONES rejected the team-list request")
	}
	if wire.Result != "SUCCESS" || wire.Data.Teams == nil || len(wire.Data.Teams) > 10000 {
		return teamCatalogBranch(ListTeamsBranchInvalidResponse, sdkgo.FailureProtocol, "ONES returned invalid team-list data")
	}
	teams := make([]Team, 0, len(wire.Data.Teams))
	for _, team := range wire.Data.Teams {
		if team.ID == "" || len(team.ID) > 256 || strings.TrimSpace(team.ID) != team.ID || team.Name == "" || len(team.Name) > 512 || team.Status == nil {
			return teamCatalogBranch(ListTeamsBranchInvalidResponse, sdkgo.FailureProtocol, "ONES returned an invalid team")
		}
		teams = append(teams, Team{ID: team.ID, Name: team.Name, Status: *team.Status})
	}
	return sdkgo.NewQueryBranch(ListTeamsBranchListed, ListTeamsOutput{Teams: teams}, nil, sdkgo.Receipt{})
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
	failure := sdkgo.Failure{Kind: kind, Provider: "ones", Operation: "listTeams", Message: message}
	return sdkgo.NewQueryBranch(branch, ListTeamsOutput{}, &failure, sdkgo.Receipt{})
}
func teamCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListTeamsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ones", Operation: "listTeams", Message: message}
	return sdkgo.NewQueryRetry[ListTeamsOutput](failure, delay)
}
