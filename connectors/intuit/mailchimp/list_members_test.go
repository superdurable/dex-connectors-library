// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListMembersSendsBoundsFiltersAndAStableOrder(t *testing.T) {
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{
			"members": []any{memberJSON("a@example.com", "subscribed"), memberJSON("b@example.com", "subscribed")}, "list_id": testListID, "total_items": 7,
		})
	})
	changedSince := time.Date(2026, 1, 21, 15, 41, 36, 0, time.FixedZone("PST", -8*3600))
	optedInSince := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	result, err := sdkgo.RunQuery(newMailchimpDexContext("list"), newMailchimpClient(t, provider.URL).ListMembers(), mailchimpConnection, mailchimp.ListMembersInput{
		ListID: testListID, Status: mailchimp.MemberStatusSubscribed, ChangedSince: &changedSince, OptedInSince: &optedInSince, PageSize: 2, Offset: 4,
	})
	require.NoError(t, err)
	require.Equal(t, mailchimp.ListMembersBranchListed, result.Branch)
	require.Len(t, result.Value.Members, 2)
	require.Equal(t, "a@example.com", result.Value.Members[0].EmailAddress)
	require.Equal(t, mailchimp.SubscriberHash("b@example.com"), result.Value.Members[1].SubscriberHash)
	require.Equal(t, 7, result.Value.TotalItems)
	require.Equal(t, 6, result.Value.NextOffset)

	request := provider.request(0)
	require.Equal(t, "/3.0/lists/"+testListID+"/members", request.path)
	require.Equal(t, []string{"2"}, request.query["count"])
	require.Equal(t, []string{"4"}, request.query["offset"])
	require.Equal(t, []string{"subscribed"}, request.query["status"])
	require.Equal(t, []string{"2026-01-21T23:41:36+00:00"}, request.query["since_last_changed"], "Mailchimp's documented ISO 8601 form, in UTC")
	require.Equal(t, []string{"2026-01-01T00:00:00+00:00"}, request.query["since_timestamp_opt"])
	require.Equal(t, []string{"last_changed"}, request.query["sort_field"])
	require.Equal(t, []string{"ASC"}, request.query["sort_dir"])
	fields := strings.Split(request.query["fields"][0], ",")
	require.Contains(t, fields, "total_items")
	require.Contains(t, fields, "members.merge_fields")
	require.NotContains(t, fields, "members._links")
}

func TestListMembersDefaultsThePageAndStopsAfterTheLastPage(t *testing.T) {
	provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeValue(t, response, http.StatusOK, map[string]any{"members": []any{memberJSON("a@example.com", "pending")}, "total_items": 1})
			return
		}
		writeValue(t, response, http.StatusOK, map[string]any{"members": []any{}, "total_items": 1})
	})
	client := newMailchimpClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newMailchimpDexContext("list-default"), client.ListMembers(), mailchimpConnection, mailchimp.ListMembersInput{ListID: testListID})
	require.NoError(t, err)
	require.Zero(t, result.Value.NextOffset, "the only contact was on this page")
	require.Equal(t, []string{"100"}, provider.request(0).query["count"])
	require.Equal(t, []string{"0"}, provider.request(0).query["offset"])
	require.Empty(t, provider.request(0).query["status"], "empty status lists every status")

	empty, err := sdkgo.RunQuery(newMailchimpDexContext("list-past-end"), client.ListMembers(), mailchimpConnection, mailchimp.ListMembersInput{ListID: testListID, Offset: 5})
	require.NoError(t, err)
	require.Empty(t, empty.Value.Members)
	require.Zero(t, empty.Value.NextOffset, "an empty page never points to another page")
}

func TestListMembersSelectsNotFoundInvalidResponseAndDefect(t *testing.T) {
	missing := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeProblem(t, response, http.StatusNotFound, "Resource Not Found")
	})
	result, err := sdkgo.RunQuery(newMailchimpDexContext("list-missing"), newMailchimpClient(t, missing.URL).ListMembers(), mailchimpConnection, mailchimp.ListMembersInput{ListID: testListID})
	require.NoError(t, err)
	require.Equal(t, mailchimp.ListMembersBranchNotFound, result.Branch)

	for name, body := range map[string]string{
		"no total":       `{"members":[]}`,
		"too many":       `{"members":[{},{}],"total_items":2}`,
		"no hash":        `{"members":[{"email_address":"a@example.com","status":"subscribed"}],"total_items":1}`,
		"not an object":  `[]`,
		"negative total": `{"members":[],"total_items":-1}`,
	} {
		provider := newRecordingMailchimp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, body)
		})
		result, err := sdkgo.RunQuery(newMailchimpDexContext("list-invalid"), newMailchimpClient(t, provider.URL).ListMembers(), mailchimpConnection,
			mailchimp.ListMembersInput{ListID: testListID, PageSize: 1})
		require.NoError(t, err, name)
		require.Equal(t, mailchimp.ListMembersBranchInvalidResponse, result.Branch, name)
	}

	unused := newRecordingMailchimp(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	zeroTime := time.Time{}
	for name, input := range map[string]mailchimp.ListMembersInput{
		"page too large":   {ListID: testListID, PageSize: 1001},
		"negative page":    {ListID: testListID, PageSize: -1},
		"negative offset":  {ListID: testListID, Offset: -1},
		"unknown status":   {ListID: testListID, Status: "deleted"},
		"zero time":        {ListID: testListID, ChangedSince: &zeroTime},
		"invalid audience": {ListID: "list id"},
	} {
		result, err := sdkgo.RunQuery(newMailchimpDexContext("list-defect"), newMailchimpClient(t, unused.URL).ListMembers(), mailchimpConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, mailchimp.ListMembersBranchDefect, result.Branch, name)
	}
}
