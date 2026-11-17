// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestActivityQueriesFilterProviderDetails(t *testing.T) {
	t.Run("paging token", func(t *testing.T) {
		client := testClient(t, func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "/rest/v1/activities/pagingtoken.json", request.URL.Path)
			require.Equal(t, "2026-01-01T00:00:00Z", request.URL.Query().Get("sinceDatetime"))
			return response(200, `{"success":true,"nextPageToken":"CURSOR","diagnostic":"secret"}`), nil
		})
		result, err := sdkgo.RunQuery(queryContext{}, client.GetActivityPagingToken(), testConnection, GetActivityPagingTokenInput{SinceDatetime: "2026-01-01T00:00:00Z"})
		require.NoError(t, err)
		require.Equal(t, GetActivityPagingTokenBranchFound, result.Branch)
		require.Equal(t, "CURSOR", result.Value.NextPageToken)
	})
	t.Run("types", func(t *testing.T) {
		client := testClient(t, func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "/rest/v1/activities/types.json", request.URL.Path)
			return response(200, `{"success":true,"result":[{"id":2,"name":"Fill Out Form","attributes":[{"name":"Client IP Address"}]}]}`), nil
		})
		result, err := sdkgo.RunQuery(queryContext{}, client.ListActivityTypes(), testConnection, ListActivityTypesInput{})
		require.NoError(t, err)
		require.Equal(t, ListActivityTypesBranchFound, result.Branch)
		require.Equal(t, ActivityTypes{Types: []ActivityType{{ID: 2, Name: "Fill Out Form"}}}, result.Value)
	})
	t.Run("activities", func(t *testing.T) {
		client := testClient(t, func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "/rest/v1/activities.json", request.URL.Path)
			require.Equal(t, "CURSOR", request.URL.Query().Get("nextPageToken"))
			require.Equal(t, "2,12", request.URL.Query().Get("activityTypeIds"))
			return response(200, `{"success":true,"nextPageToken":"NEXT","moreResult":false,"result":[{"id":42,"leadId":318581,"activityTypeId":2,"activityDate":"2026-01-01T00:00:00Z","primaryAttributeValue":"secret","attributes":[{"name":"Form Fields","value":"secret"}]}]}`), nil
		})
		result, err := sdkgo.RunQuery(queryContext{}, client.ListActivities(), testConnection, ListActivitiesInput{NextPageToken: "CURSOR", ActivityTypeIDs: []int64{2, 12}})
		require.NoError(t, err)
		require.Equal(t, ListActivitiesBranchFound, result.Branch)
		require.Equal(t, ActivityPage{Activities: []ActivityMetadata{{MarketoGUID: "42", LeadID: 318581, ActivityTypeID: 2, ActivityDate: "2026-01-01T00:00:00Z"}}, NextPageToken: "NEXT"}, result.Value)
	})
}

func TestActivityQueriesClassifyResponses(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "provider rejected", status: 200, body: `{"success":false,"errors":[{"code":"603","message":"secret"}]}`, branch: ListActivitiesBranchProviderRejected},
		{name: "invalid JSON", status: 200, body: `{`, branch: ListActivitiesBranchInvalidResponse},
		{name: "missing result", status: 200, body: `{"success":true,"nextPageToken":"NEXT","moreResult":false}`, branch: ListActivitiesBranchInvalidResponse},
		{name: "rate limited", status: 429, body: `{}`, retry: true},
		{name: "unavailable", status: 503, body: `{}`, retry: true},
		{name: "server error", status: 500, body: `{}`, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(*http.Request) (*http.Response, error) { return response(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, client.ListActivities(), testConnection, ListActivitiesInput{NextPageToken: "CURSOR", ActivityTypeIDs: []int64{2}})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "secret")
		})
	}
	client := testClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("secret network details") })
	_, err := sdkgo.RunQuery(queryContext{}, client.ListActivityTypes(), testConnection, ListActivityTypesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestActivityQueriesValidateInputs(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected provider call"); return nil, nil })
	result, err := sdkgo.RunQuery(queryContext{}, client.GetActivityPagingToken(), testConnection, GetActivityPagingTokenInput{SinceDatetime: "yesterday"})
	require.NoError(t, err)
	require.Equal(t, GetActivityPagingTokenBranchDefect, result.Branch)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListActivities(), testConnection, ListActivitiesInput{NextPageToken: "CURSOR"})
	require.NoError(t, err)
	require.Equal(t, ListActivitiesBranchDefect, page.Branch)
}
