// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mixpanel

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// QueryProfilesInput selects a user profile by its distinct ID.
type QueryProfilesInput struct {
	// DistinctID is the user identity in Mixpanel.
	DistinctID string `json:"distinctID"`
}

// QueriedProfiles contains only identities, never arbitrary profile properties.
type QueriedProfiles struct {
	// DistinctIDs contains the matching profile identities.
	DistinctIDs []string `json:"distinctIDs"`
}

// ListSavedCohortsInput bounds the number of saved cohorts returned to a Flow.
type ListSavedCohortsInput struct {
	// Limit is the maximum number of cohorts, from 1 through 1000.
	Limit int `json:"limit"`
}

// SavedCohort contains non-secret metadata about a saved cohort.
type SavedCohort struct {
	// ID is the Mixpanel cohort identifier.
	ID int64 `json:"id"`
	// Name is the cohort display name.
	Name string `json:"name"`
	// Count is the current approximate member count.
	Count int64 `json:"count"`
}

// SavedCohorts contains a bounded list of saved cohort metadata.
type SavedCohorts struct {
	// Cohorts are returned in Mixpanel's order.
	Cohorts []SavedCohort `json:"cohorts"`
}

// QueryProfilesOperation queries profile identities.
type QueryProfilesOperation struct{ client *Client }

// ListSavedCohortsOperation lists cohort metadata.
type ListSavedCohortsOperation struct{ client *Client }

// QueryProfiles binds the profile query to this client.
func (client *Client) QueryProfiles() QueryProfilesOperation { return QueryProfilesOperation{client} }

// ListSavedCohorts binds cohort listing to this client.
func (client *Client) ListSavedCohorts() ListSavedCohortsOperation {
	return ListSavedCohortsOperation{client}
}

// Definition returns the generated profile query contract.
func (QueryProfilesOperation) Definition() sdkgo.QueryDefinition { return QueryProfilesDefinition }

// Definition returns the generated cohort list contract.
func (ListSavedCohortsOperation) Definition() sdkgo.QueryDefinition {
	return ListSavedCohortsDefinition
}

// Invoke queries one user profile and excludes arbitrary profile properties.
func (operation QueryProfilesOperation) Invoke(call sdkgo.Call, input QueryProfilesInput) sdkgo.QueryAttempt[QueriedProfiles] {
	const name = "queryProfiles"
	if strings.TrimSpace(input.DistinctID) == "" {
		return sdkgo.NewQueryBranch(QueryProfilesBranchDefect, QueriedProfiles{}, mixpanelFailure(name, sdkgo.FailureValidation, "distinctID is required"), sdkgo.Receipt{})
	}
	query := url.Values{"project_id": {strconv.FormatInt(operation.client.projectID, 10)}}
	form := url.Values{"distinct_id": {input.DistinctID}}
	body, status, failure, retryAfter := operation.client.send(call, http.MethodPost, operation.client.queryBaseURL+"/engage?"+query.Encode(), []byte(form.Encode()), name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewQueryBranch(QueryProfilesBranchDefect, QueriedProfiles{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewQueryRetry[QueriedProfiles](*failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(QueryProfilesBranchInvalidResponse, QueriedProfiles{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(QueryProfilesBranchProviderRejected, QueriedProfiles{}, mixpanelFailure(name, rejectionKind(status), "Mixpanel rejected profile query with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var answer struct {
		Status  string `json:"status"`
		Results []struct {
			DistinctID json.RawMessage `json:"$distinct_id"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.Status != "ok" || answer.Results == nil || len(answer.Results) > 1 {
		return sdkgo.NewQueryBranch(QueryProfilesBranchInvalidResponse, QueriedProfiles{}, mixpanelFailure(name, sdkgo.FailureProtocol, "Mixpanel returned an invalid profile query response"), sdkgo.Receipt{})
	}
	result := QueriedProfiles{DistinctIDs: []string{}}
	for _, profile := range answer.Results {
		var distinctID string
		if json.Unmarshal(profile.DistinctID, &distinctID) != nil {
			var numericID json.Number
			if json.Unmarshal(profile.DistinctID, &numericID) == nil {
				distinctID = numericID.String()
			}
		}
		if distinctID == "" {
			return sdkgo.NewQueryBranch(QueryProfilesBranchInvalidResponse, QueriedProfiles{}, mixpanelFailure(name, sdkgo.FailureProtocol, "Mixpanel returned an invalid profile identity"), sdkgo.Receipt{})
		}
		result.DistinctIDs = append(result.DistinctIDs, distinctID)
	}
	return sdkgo.NewQueryBranch(QueryProfilesBranchQueried, result, nil, sdkgo.Receipt{})
}

// Invoke lists saved cohorts and strips descriptions and other free-form fields.
func (operation ListSavedCohortsOperation) Invoke(call sdkgo.Call, input ListSavedCohortsInput) sdkgo.QueryAttempt[SavedCohorts] {
	const name = "listSavedCohorts"
	if input.Limit < 1 || input.Limit > 1000 {
		return sdkgo.NewQueryBranch(ListSavedCohortsBranchDefect, SavedCohorts{}, mixpanelFailure(name, sdkgo.FailureValidation, "limit must be from 1 through 1000"), sdkgo.Receipt{})
	}
	query := url.Values{"project_id": {strconv.FormatInt(operation.client.projectID, 10)}}
	body, status, failure, retryAfter := operation.client.send(call, http.MethodPost, operation.client.queryBaseURL+"/cohorts/list?"+query.Encode(), nil, name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewQueryBranch(ListSavedCohortsBranchDefect, SavedCohorts{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewQueryRetry[SavedCohorts](*failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(ListSavedCohortsBranchInvalidResponse, SavedCohorts{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListSavedCohortsBranchProviderRejected, SavedCohorts{}, mixpanelFailure(name, rejectionKind(status), "Mixpanel rejected cohort query with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var cohorts []SavedCohort
	if json.Unmarshal(body, &cohorts) != nil || cohorts == nil {
		return sdkgo.NewQueryBranch(ListSavedCohortsBranchInvalidResponse, SavedCohorts{}, mixpanelFailure(name, sdkgo.FailureProtocol, "Mixpanel returned invalid saved cohorts"), sdkgo.Receipt{})
	}
	result := SavedCohorts{Cohorts: []SavedCohort{}}
	for _, cohort := range cohorts {
		if cohort.ID <= 0 || cohort.Name == "" || cohort.Count < 0 {
			return sdkgo.NewQueryBranch(ListSavedCohortsBranchInvalidResponse, SavedCohorts{}, mixpanelFailure(name, sdkgo.FailureProtocol, "Mixpanel returned invalid cohort metadata"), sdkgo.Receipt{})
		}
		if len(result.Cohorts) < input.Limit {
			result.Cohorts = append(result.Cohorts, cohort)
		}
	}
	return sdkgo.NewQueryBranch(ListSavedCohortsBranchListed, result, nil, sdkgo.Receipt{})
}
