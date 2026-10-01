// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListMeetingsReadsOneBoundedPage(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"page_size":2,"total_records":3,"next_page_token":"Tva2CuIdTgsv8wAnhyAdU3m06Y2HuLQtlh3",`+
			`"meetings":[`+meetingJSON(1234567890, "Customer sync", "2036-02-20T22:00:00Z")+`,`+
			`{"id":5544332211,"uuid":"u2","topic":"Standing check-in","type":3,"duration":0,"join_url":"https://us05web.zoom.us/j/5544332211"}]}`)
	})
	client := newZoomClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newZoomDexContext("list"), client.ListMeetings(), zoomConnection, zoom.ListMeetingsInput{
		Type: zoom.MeetingListScheduled, PageSize: 2, PageToken: "IAfJX3jsOLW7w3dokmFl84zOa0MAVGyMEB2",
	})
	require.NoError(t, err)
	require.Equal(t, zoom.ListMeetingsBranchListed, result.Branch)
	request := provider.request(t, 0)
	require.Equal(t, "/users/me/meetings", request.path)
	require.Equal(t, map[string][]string{
		"type": {"scheduled"}, "page_size": {"2"}, "next_page_token": {"IAfJX3jsOLW7w3dokmFl84zOa0MAVGyMEB2"},
	}, request.query)

	page := result.Value
	require.Equal(t, 3, page.TotalRecords)
	require.Equal(t, "Tva2CuIdTgsv8wAnhyAdU3m06Y2HuLQtlh3", page.NextPageToken)
	require.Len(t, page.Meetings, 2)
	require.Equal(t, int64(1234567890), page.Meetings[0].ID)
	require.True(t, page.Meetings[0].StartTime.Equal(time.Date(2036, time.February, 20, 22, 0, 0, 0, time.UTC)))
	require.Equal(t, 30, page.Meetings[0].DurationMinutes)
	require.Nil(t, page.Meetings[1].StartTime, "a recurring meeting without a fixed time has no start")
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
}

func TestListMeetingsDefaultsToUpcomingMeetingsInZoomsDefaultPage(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"total_records":0,"meetings":[]}`)
	})
	result, err := sdkgo.RunQuery(newZoomDexContext("list-defaults"), newZoomClient(t, provider.URL).ListMeetings(), zoomConnection, zoom.ListMeetingsInput{})
	require.NoError(t, err)
	require.Equal(t, zoom.ListMeetingsBranchListed, result.Branch)
	require.Equal(t, map[string][]string{"type": {"upcoming"}, "page_size": {"30"}}, provider.request(t, 0).query)
	require.Empty(t, result.Value.Meetings)
	require.NotNil(t, result.Value.Meetings, "an empty page is an empty list, not null")
	require.Empty(t, result.Value.NextPageToken)
}

func TestListMeetingsRejectsUnboundedOrUnknownInputWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {
		t.Fatal("invalid input must not reach Zoom")
	})
	client := newZoomClient(t, provider.URL)
	for _, input := range []zoom.ListMeetingsInput{
		{PageSize: 301}, {PageSize: -1}, {Type: "upcoming_meetings"}, {Type: "everything"}, {PageToken: "has space"},
	} {
		result, err := sdkgo.RunQuery(newZoomDexContext("list-invalid"), client.ListMeetings(), zoomConnection, input)
		require.NoError(t, err)
		require.Equal(t, zoom.ListMeetingsBranchDefect, result.Branch, "%+v", input)
	}
	require.Zero(t, provider.requestCount())
}

func TestListMeetingsClassifiesUnusablePagesAndRejections(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "meeting without an ID", status: http.StatusOK, body: `{"meetings":[{"topic":"x"}]}`, branch: zoom.ListMeetingsBranchInvalidResponse},
		{name: "naive start time", status: http.StatusOK, body: `{"meetings":[{"id":1,"start_time":"2036-02-20T22:00:00"}]}`, branch: zoom.ListMeetingsBranchInvalidResponse},
		{name: "insecure join URL", status: http.StatusOK, body: `{"meetings":[{"id":1,"join_url":"http://zoom.example/j/1"}]}`, branch: zoom.ListMeetingsBranchInvalidResponse},
		{name: "trailing data", status: http.StatusOK, body: `{"meetings":[]}{}`, branch: zoom.ListMeetingsBranchInvalidResponse},
		{name: "cannot host", status: http.StatusBadRequest, body: `{"code":3161,"message":"SENTINEL"}`, branch: zoom.ListMeetingsBranchProviderRejected},
		{name: "unknown user", status: http.StatusNotFound, body: `{"code":1001,"message":"SENTINEL"}`, branch: zoom.ListMeetingsBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newZoomDexContext("list-"+test.name), newZoomClient(t, provider.URL).ListMeetings(), zoomConnection, zoom.ListMeetingsInput{})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Empty(t, result.Value.Meetings)
		})
	}
}
