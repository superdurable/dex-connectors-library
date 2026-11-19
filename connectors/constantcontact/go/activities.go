// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetActivityInput identifies a recent bulk activity by its provider UUID.
type GetActivityInput struct {
	// ActivityID is returned by a bulk operation and expires after ten days.
	ActivityID string `json:"activityId"`
}

// ListActivitiesInput bounds the retained activity summaries returned to Dex.
type ListActivitiesInput struct {
	// MaxItems limits returned summaries to 1 through 1000; zero uses 1000.
	MaxItems int `json:"maxItems,omitempty"`
}

// ActivityMetadata holds public progress fields without provider error text.
type ActivityMetadata struct {
	// ActivityID is the provider UUID.
	ActivityID string `json:"activityId"`
	// State is the provider's current job state.
	State string `json:"state"`
	// PercentDone is the provider-reported completion percentage.
	PercentDone int `json:"percentDone"`
	// ItemsTotalCount is the number of requested items when known.
	ItemsTotalCount int `json:"itemsTotalCount,omitempty"`
	// ItemsCompletedCount is the number of completed items when known.
	ItemsCompletedCount int `json:"itemsCompletedCount,omitempty"`
}

// ActivitiesPage contains recent bulk activity summaries.
type ActivitiesPage struct {
	// Activities is the provider's returned collection.
	Activities []ActivityMetadata `json:"activities"`
}

// GetActivityOperation reads one recent activity.
type GetActivityOperation struct{ client *Client }

// ListActivitiesOperation reads the provider's recent activity collection.
type ListActivitiesOperation struct{ client *Client }

// GetActivity binds the Query to this client.
func (client *Client) GetActivity() GetActivityOperation { return GetActivityOperation{client} }

// ListActivities binds the Query to this client.
func (client *Client) ListActivities() ListActivitiesOperation {
	return ListActivitiesOperation{client}
}

// Definition returns the generated Query contract.
func (GetActivityOperation) Definition() sdkgo.QueryDefinition { return GetActivityDefinition }

// Definition returns the generated Query contract.
func (ListActivitiesOperation) Definition() sdkgo.QueryDefinition { return ListActivitiesDefinition }

// Invoke reads one activity and classifies provider outcomes.
func (operation GetActivityOperation) Invoke(call sdkgo.Call, input GetActivityInput) sdkgo.QueryAttempt[ActivityMetadata] {
	if !contactIDPattern.MatchString(input.ActivityID) {
		return listQueryBranch[ActivityMetadata]("getActivity", GetActivityBranchDefect, sdkgo.FailureValidation, "activity ID must be a UUID")
	}
	response := operation.client.get(call, "/activities/"+input.ActivityID)
	if kind, failure := classifyListResponse("getActivity", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewQueryRetry[ActivityMetadata](failure, response.retryAfter)
		}
		branch := GetActivityBranchProviderRejected
		if kind == listResponseDefect {
			branch = GetActivityBranchDefect
		} else if kind == listResponseInvalid {
			branch = GetActivityBranchInvalidResponse
		} else if response.status == http.StatusNotFound {
			branch = GetActivityBranchNotFound
		}
		return sdkgo.NewQueryBranch(branch, ActivityMetadata{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listQueryBranch[ActivityMetadata]("getActivity", GetActivityBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected the activity lookup")
	}
	activity, err := decodeActivity(response.body)
	if err != nil {
		return listQueryBranch[ActivityMetadata]("getActivity", GetActivityBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid activity")
	}
	return sdkgo.NewQueryBranch(GetActivityBranchFound, activity, nil, sdkgo.Receipt{})
}

// Invoke reads the recent activity collection and classifies provider outcomes.
func (operation ListActivitiesOperation) Invoke(call sdkgo.Call, input ListActivitiesInput) sdkgo.QueryAttempt[ActivitiesPage] {
	if input.MaxItems < 0 || input.MaxItems > 1000 {
		return listQueryBranch[ActivitiesPage]("listActivities", ListActivitiesBranchDefect, sdkgo.FailureValidation, "maxItems must be 1 through 1000")
	}
	response := operation.client.get(call, "/activities")
	if kind, failure := classifyListResponse("listActivities", response); kind != listResponseSuccess {
		if kind == listResponseRetry {
			return sdkgo.NewQueryRetry[ActivitiesPage](failure, response.retryAfter)
		}
		branch := ListActivitiesBranchProviderRejected
		if kind == listResponseDefect {
			branch = ListActivitiesBranchDefect
		} else if kind == listResponseInvalid {
			branch = ListActivitiesBranchInvalidResponse
		}
		return sdkgo.NewQueryBranch(branch, ActivitiesPage{}, &failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listQueryBranch[ActivitiesPage]("listActivities", ListActivitiesBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected the activity collection")
	}
	var envelope struct {
		Activities []json.RawMessage `json:"activities"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Activities == nil {
		return listQueryBranch[ActivitiesPage]("listActivities", ListActivitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid activity collection")
	}
	page := ActivitiesPage{Activities: make([]ActivityMetadata, 0, len(envelope.Activities))}
	for _, rawActivity := range envelope.Activities {
		activity, err := decodeActivity(rawActivity)
		if err != nil {
			return listQueryBranch[ActivitiesPage]("listActivities", ListActivitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Constant Contact returned an invalid activity")
		}
		if input.MaxItems == 0 || len(page.Activities) < input.MaxItems {
			page.Activities = append(page.Activities, activity)
		}
	}
	return sdkgo.NewQueryBranch(ListActivitiesBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeActivity(body []byte) (ActivityMetadata, error) {
	var raw struct {
		ActivityID  string `json:"activity_id"`
		State       string `json:"state"`
		PercentDone int    `json:"percent_done"`
		Status      struct {
			ItemsTotalCount     int `json:"items_total_count"`
			ItemsCompletedCount int `json:"items_completed_count"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ActivityMetadata{}, err
	}
	if !contactIDPattern.MatchString(raw.ActivityID) || strings.TrimSpace(raw.State) == "" || raw.PercentDone < 0 || raw.PercentDone > 100 || raw.Status.ItemsTotalCount < 0 || raw.Status.ItemsCompletedCount < 0 {
		return ActivityMetadata{}, errors.New("activity fields missing or invalid")
	}
	return ActivityMetadata{ActivityID: raw.ActivityID, State: raw.State, PercentDone: raw.PercentDone, ItemsTotalCount: raw.Status.ItemsTotalCount, ItemsCompletedCount: raw.Status.ItemsCompletedCount}, nil
}
