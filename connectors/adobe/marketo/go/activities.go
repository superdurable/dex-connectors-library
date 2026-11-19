// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

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

// GetActivityPagingTokenInput selects the first activity timestamp to read.
type GetActivityPagingTokenInput struct {
	// SinceDatetime is an RFC 3339 timestamp with a timezone.
	SinceDatetime string `json:"sinceDatetime"`
}

// ActivityPagingToken is a Marketo cursor for activity queries.
type ActivityPagingToken struct {
	// NextPageToken is passed to ListActivities.
	NextPageToken string `json:"nextPageToken"`
}

// ListActivityTypesInput requests all activity types in a subscription.
type ListActivityTypesInput struct{}

// ActivityType identifies an activity kind without exposing its attribute schema.
type ActivityType struct {
	// ID is the Marketo activity type ID.
	ID int64 `json:"id"`
	// Name is the activity type's display name.
	Name string `json:"name"`
}

// ActivityTypes contains the subscription's available activity types.
type ActivityTypes struct {
	// Types contains only IDs and display names.
	Types []ActivityType `json:"types"`
}

// ListActivitiesInput selects one page and up to ten activity types.
type ListActivitiesInput struct {
	// NextPageToken comes from GetActivityPagingToken or the preceding page.
	NextPageToken string `json:"nextPageToken"`
	// ActivityTypeIDs contains one to ten IDs from ListActivityTypes.
	ActivityTypeIDs []int64 `json:"activityTypeIds"`
}

// ActivityMetadata omits provider attributes, form fields, and URL values.
type ActivityMetadata struct {
	// MarketoGUID uniquely identifies an activity; older responses use the numeric ID.
	MarketoGUID string `json:"marketoGUID"`
	// LeadID identifies the lead associated with the activity.
	LeadID int64 `json:"leadId"`
	// ActivityTypeID identifies the activity type.
	ActivityTypeID int64 `json:"activityTypeId"`
	// ActivityDate is the provider timestamp.
	ActivityDate string `json:"activityDate"`
}

// ActivityPage contains safe activity metadata and the next cursor.
type ActivityPage struct {
	// Activities contains one page of activity metadata.
	Activities []ActivityMetadata `json:"activities"`
	// NextPageToken is the cursor for the following page.
	NextPageToken string `json:"nextPageToken"`
	// MoreResult is true while Marketo has more records in this result set.
	MoreResult bool `json:"moreResult"`
}

// GetActivityPagingTokenOperation reads a date-based activity cursor.
type GetActivityPagingTokenOperation struct{ client *Client }

// GetActivityPagingToken binds the Query to this client.
func (client *Client) GetActivityPagingToken() GetActivityPagingTokenOperation {
	return GetActivityPagingTokenOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetActivityPagingTokenOperation) Definition() sdkgo.QueryDefinition {
	return GetActivityPagingTokenDefinition
}

// Invoke reads the cursor without exposing provider diagnostic details.
func (operation GetActivityPagingTokenOperation) Invoke(call sdkgo.Call, input GetActivityPagingTokenInput) sdkgo.QueryAttempt[ActivityPagingToken] {
	if _, err := time.Parse(time.RFC3339, input.SinceDatetime); err != nil {
		return activityBranch[ActivityPagingToken]("getActivityPagingToken", GetActivityPagingTokenBranchDefect, sdkgo.FailureValidation, "activity timestamp must use RFC 3339 with a timezone")
	}
	path := "/v1/activities/pagingtoken.json?" + url.Values{"sinceDatetime": {input.SinceDatetime}}.Encode()
	return activityQuery(call, operation.client, path, "getActivityPagingToken", GetActivityPagingTokenBranchFound, GetActivityPagingTokenBranchProviderRejected, GetActivityPagingTokenBranchInvalidResponse, func(envelope activityEnvelope) (ActivityPagingToken, bool) {
		return ActivityPagingToken{NextPageToken: envelope.NextPageToken}, validActivityToken(envelope.NextPageToken)
	})
}

// ListActivityTypesOperation reads the available activity type IDs and names.
type ListActivityTypesOperation struct{ client *Client }

// ListActivityTypes binds the Query to this client.
func (client *Client) ListActivityTypes() ListActivityTypesOperation {
	return ListActivityTypesOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListActivityTypesOperation) Definition() sdkgo.QueryDefinition {
	return ListActivityTypesDefinition
}

// Invoke reads activity types and discards potentially sensitive attribute definitions.
func (operation ListActivityTypesOperation) Invoke(call sdkgo.Call, _ ListActivityTypesInput) sdkgo.QueryAttempt[ActivityTypes] {
	return activityQuery(call, operation.client, "/v1/activities/types.json", "listActivityTypes", ListActivityTypesBranchFound, ListActivityTypesBranchProviderRejected, ListActivityTypesBranchInvalidResponse, func(envelope activityEnvelope) (ActivityTypes, bool) {
		var types []ActivityType
		if len(envelope.Result) == 0 || json.Unmarshal(envelope.Result, &types) != nil || types == nil {
			return ActivityTypes{}, false
		}
		for _, activityType := range types {
			if activityType.ID <= 0 || activityType.Name == "" {
				return ActivityTypes{}, false
			}
		}
		return ActivityTypes{Types: types}, true
	})
}

// ListActivitiesOperation reads one page of safe activity metadata.
type ListActivitiesOperation struct{ client *Client }

// ListActivities binds the Query to this client.
func (client *Client) ListActivities() ListActivitiesOperation {
	return ListActivitiesOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListActivitiesOperation) Definition() sdkgo.QueryDefinition {
	return ListActivitiesDefinition
}

// Invoke reads one page while omitting provider attributes and primary values.
func (operation ListActivitiesOperation) Invoke(call sdkgo.Call, input ListActivitiesInput) sdkgo.QueryAttempt[ActivityPage] {
	if !validActivityToken(input.NextPageToken) || len(input.ActivityTypeIDs) == 0 || len(input.ActivityTypeIDs) > 10 {
		return activityBranch[ActivityPage]("listActivities", ListActivitiesBranchDefect, sdkgo.FailureValidation, "activity cursor or type IDs are invalid")
	}
	identifiers := make([]string, 0, len(input.ActivityTypeIDs))
	for _, identifier := range input.ActivityTypeIDs {
		if identifier <= 0 {
			return activityBranch[ActivityPage]("listActivities", ListActivitiesBranchDefect, sdkgo.FailureValidation, "activity type ID must be positive")
		}
		identifiers = append(identifiers, strconv.FormatInt(identifier, 10))
	}
	path := "/v1/activities.json?" + url.Values{"nextPageToken": {input.NextPageToken}, "activityTypeIds": {strings.Join(identifiers, ",")}}.Encode()
	return activityQuery(call, operation.client, path, "listActivities", ListActivitiesBranchFound, ListActivitiesBranchProviderRejected, ListActivitiesBranchInvalidResponse, func(envelope activityEnvelope) (ActivityPage, bool) {
		var activities []struct {
			ID int64 `json:"id"`
			ActivityMetadata
		}
		if len(envelope.Result) == 0 || json.Unmarshal(envelope.Result, &activities) != nil || activities == nil || envelope.MoreResult == nil || !validActivityToken(envelope.NextPageToken) {
			return ActivityPage{}, false
		}
		page := ActivityPage{Activities: make([]ActivityMetadata, 0, len(activities)), NextPageToken: envelope.NextPageToken, MoreResult: *envelope.MoreResult}
		for _, activity := range activities {
			if activity.MarketoGUID == "" && activity.ID > 0 {
				activity.MarketoGUID = strconv.FormatInt(activity.ID, 10)
			}
			if activity.MarketoGUID == "" || activity.LeadID <= 0 || activity.ActivityTypeID <= 0 || activity.ActivityDate == "" {
				return ActivityPage{}, false
			}
			page.Activities = append(page.Activities, activity.ActivityMetadata)
		}
		return page, true
	})
}

type activityEnvelope struct {
	Success       *bool           `json:"success"`
	NextPageToken string          `json:"nextPageToken"`
	MoreResult    *bool           `json:"moreResult"`
	Result        json.RawMessage `json:"result"`
	Errors        []struct {
		Code string `json:"code"`
	} `json:"errors"`
}

func activityQuery[T any](call sdkgo.Call, client *Client, path, operation string, found, rejected, invalid sdkgo.BranchID, decode func(activityEnvelope) (T, bool)) sdkgo.QueryAttempt[T] {
	response := client.requestLeadAPI(call, http.MethodGet, path, nil)
	if errors.Is(response.err, errLeadReauthorization) {
		return activityBranch[T](operation, rejected, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if errors.Is(response.err, errLeadCredentials) || errors.Is(response.err, errLeadTransport) {
		return activityRetry[T](operation, sdkgo.FailureTransport, "Marketo request or credential refresh failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return activityRetry[T](operation, sdkgo.FailureRateLimit, "Marketo rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return activityRetry[T](operation, sdkgo.FailureAvailability, "Marketo is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return activityBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Marketo rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return activityBranch[T](operation, invalid, sdkgo.FailureResponseTooLarge, "Marketo response exceeded the configured limit")
	}
	if response.err != nil {
		return activityRetry[T](operation, sdkgo.FailureTransport, "Marketo response was interrupted", 0)
	}
	var envelope activityEnvelope
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Success == nil {
		return activityBranch[T](operation, invalid, sdkgo.FailureProtocol, "Marketo returned invalid activity JSON")
	}
	if !*envelope.Success {
		if len(envelope.Errors) == 0 {
			return activityBranch[T](operation, invalid, sdkgo.FailureProtocol, "Marketo returned an error without code")
		}
		switch envelope.Errors[0].Code {
		case "602", "606", "607", "615":
			return activityRetry[T](operation, sdkgo.FailureRateLimit, "Marketo token or quota temporarily blocked the request", response.retryAfter)
		case "604", "608", "611", "614", "500", "502":
			return activityRetry[T](operation, sdkgo.FailureAvailability, "Marketo is temporarily unavailable", response.retryAfter)
		default:
			return activityBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Marketo rejected the activity request")
		}
	}
	value, valid := decode(envelope)
	if !valid {
		return activityBranch[T](operation, invalid, sdkgo.FailureProtocol, "Marketo returned incomplete activity data")
	}
	return sdkgo.NewQueryBranch(found, value, nil, sdkgo.Receipt{})
}

func validActivityToken(token string) bool {
	return token != "" && len(token) <= 2048 && !strings.ContainsAny(token, "\r\n")
}

func activityBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: operation, Message: message}
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func activityRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: operation, Message: message}, delay)
}
