// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ActivitySummary contains the stable identity and title of one Creatio activity.
type ActivitySummary struct {
	// ID is the Activity.Id UUID.
	ID string `json:"id"`
	// Title is the activity display title.
	Title string `json:"title"`
}

// ListActivitiesInput selects one bounded page of Creatio activities.
type ListActivitiesInput struct {
	// PageNumber starts at one; zero selects the first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero selects 20 records.
	PageSize int `json:"pageSize,omitempty"`
}

// ActivitiesPage contains one Creatio OData result page.
type ActivitiesPage struct {
	// Activities are the selected public activity fields.
	Activities []ActivitySummary `json:"activities"`
}

// ListActivitiesOperation reads one OData page without changing records.
type ListActivitiesOperation struct{ client *Client }

// ListActivities binds the Query to this client.
func (client *Client) ListActivities() ListActivitiesOperation {
	return ListActivitiesOperation{client}
}

// Definition returns the generated Query contract.
func (ListActivitiesOperation) Definition() sdkgo.QueryDefinition { return ListActivitiesDefinition }

// Invoke reads activity IDs and titles from one OData page.
func (operation ListActivitiesOperation) Invoke(call sdkgo.Call, input ListActivitiesInput) sdkgo.QueryAttempt[ActivitiesPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 {
		return activitiesPageBranch(ListActivitiesBranchDefect, sdkgo.FailureValidation, "invalid activity page")
	}
	pageNumber, pageSize := input.PageNumber, input.PageSize
	if pageNumber == 0 {
		pageNumber = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return activitiesPageBranch(ListActivitiesBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[ActivitiesPage](activitiesFailure("listActivities", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	query := url.Values{"$select": {"Id,Title"}, "$top": {strconv.Itoa(pageSize)}, "$skip": {strconv.Itoa((pageNumber - 1) * pageSize)}, "$orderby": {"Id"}}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Activity?"+query.Encode(), nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ActivitiesPage](activitiesFailure("listActivities", sdkgo.FailureRateLimit, "Creatio rate limited activities"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ActivitiesPage](activitiesFailure("listActivities", sdkgo.FailureAvailability, "Creatio activities are unavailable"), response.retryAfter)
	}
	if response.isBodyTooLarge {
		return activitiesPageBranch(ListActivitiesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio activities response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ActivitiesPage](activitiesFailure("listActivities", sdkgo.FailureTransport, "Creatio activities response failed"), 0)
	}
	if response.status != http.StatusOK {
		return activitiesPageBranch(ListActivitiesBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected activities HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return activitiesPageBranch(ListActivitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid activities JSON")
	}
	page := ActivitiesPage{Activities: make([]ActivitySummary, 0, len(envelope.Value))}
	for _, record := range envelope.Value {
		activity, err := decodeActivityRecord(record)
		if err != nil {
			return activitiesPageBranch(ListActivitiesBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned an invalid activity")
		}
		page.Activities = append(page.Activities, activity)
	}
	return sdkgo.NewQueryBranch(ListActivitiesBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeActivityRecord(body []byte) (ActivitySummary, error) {
	var wire struct {
		ID    string `json:"Id"`
		Title string `json:"Title"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ActivitySummary{}, err
	}
	if !contactUUIDPattern.MatchString(wire.ID) || strings.TrimSpace(wire.Title) == "" {
		return ActivitySummary{}, errors.New("activity identity missing")
	}
	return ActivitySummary{ID: wire.ID, Title: wire.Title}, nil
}

func activitiesFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "creatio", Operation: operation, Message: message}
}

func activitiesPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ActivitiesPage] {
	failure := activitiesFailure("listActivities", kind, message)
	return sdkgo.NewQueryBranch(branch, ActivitiesPage{}, &failure, sdkgo.Receipt{})
}
