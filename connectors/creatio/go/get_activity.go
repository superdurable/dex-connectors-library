// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetActivityInput identifies one Creatio activity by UUID.
type GetActivityInput struct {
	// ActivityID is the Activity.Id UUID.
	ActivityID string `json:"activityId"`
}

// GetActivityOperation reads one activity through OData 4.
type GetActivityOperation struct{ client *Client }

// GetActivity binds the Query to this client.
func (client *Client) GetActivity() GetActivityOperation { return GetActivityOperation{client} }

// Definition returns the generated Query contract.
func (GetActivityOperation) Definition() sdkgo.QueryDefinition { return GetActivityDefinition }

// Invoke reads an activity's stable ID and title.
func (operation GetActivityOperation) Invoke(call sdkgo.Call, input GetActivityInput) sdkgo.QueryAttempt[ActivitySummary] {
	if !contactUUIDPattern.MatchString(input.ActivityID) {
		return activityQueryBranch(GetActivityBranchDefect, sdkgo.FailureValidation, "invalid activity UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return activityQueryBranch(GetActivityBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[ActivitySummary](activitiesFailure("getActivity", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Activity("+input.ActivityID+")?$select=Id%2CTitle", nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ActivitySummary](activitiesFailure("getActivity", sdkgo.FailureRateLimit, "Creatio rate limited activity lookup"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ActivitySummary](activitiesFailure("getActivity", sdkgo.FailureAvailability, "Creatio activity lookup unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return activityQueryBranch(GetActivityBranchNotFound, sdkgo.FailureNotFound, "Creatio activity was not found")
	}
	if response.isBodyTooLarge {
		return activityQueryBranch(GetActivityBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio activity response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ActivitySummary](activitiesFailure("getActivity", sdkgo.FailureTransport, "Creatio activity response failed"), 0)
	}
	if response.status != http.StatusOK {
		return activityQueryBranch(GetActivityBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	activity, err := decodeActivityRecord(response.body)
	if err != nil || !strings.EqualFold(activity.ID, input.ActivityID) {
		return activityQueryBranch(GetActivityBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid activity JSON")
	}
	return sdkgo.NewQueryBranch(GetActivityBranchFound, activity, nil, sdkgo.Receipt{})
}

func activityQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ActivitySummary] {
	failure := activitiesFailure("getActivity", kind, message)
	return sdkgo.NewQueryBranch(branch, ActivitySummary{}, &failure, sdkgo.Receipt{})
}
