// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListPastMeetingParticipantsReadsOnePage(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"page_count":1,"page_size":300,"total_records":2,"next_page_token":"",`+
			`"participants":[{"id":"30R7kT7bTIKSNUFEuH_Qlg","name":"Host","user_id":"27423744","user_email":"host@example.com",`+
			`"join_time":"2026-09-30T17:00:03Z","leave_time":"2026-09-30T17:31:00Z","duration":1857,"status":"in_meeting","internal_user":true},`+
			`{"id":"","name":"Priya","user_email":"","join_time":"2026-09-30T17:01:10Z","leave_time":"2026-09-30T17:30:00Z","duration":1730,"failover":false}]}`)
	})
	result, err := sdkgo.RunQuery(newZoomDexContext("participants"), newZoomClient(t, provider.URL).ListPastMeetingParticipants(), zoomConnection,
		zoom.ListPastMeetingParticipantsInput{MeetingID: 85746065, PageSize: 300})
	require.NoError(t, err)
	require.Equal(t, zoom.ListPastMeetingParticipantsBranchListed, result.Branch)
	request := provider.request(t, 0)
	require.Equal(t, "/past_meetings/85746065/participants", request.path)
	require.Equal(t, map[string][]string{"page_size": {"300"}}, request.query)

	page := result.Value
	require.Equal(t, int64(85746065), page.MeetingID)
	require.Equal(t, 2, page.TotalRecords)
	require.Len(t, page.Participants, 2)
	require.Equal(t, zoom.MeetingParticipant{
		ID: "30R7kT7bTIKSNUFEuH_Qlg", Name: "Host", Email: "host@example.com",
		JoinTime:        timePointer(time.Date(2026, time.September, 30, 17, 0, 3, 0, time.UTC)),
		LeaveTime:       timePointer(time.Date(2026, time.September, 30, 17, 31, 0, 0, time.UTC)),
		DurationSeconds: 1857, Status: "in_meeting", IsInternalUser: true,
	}, page.Participants[0])
	require.Empty(t, page.Participants[1].ID, "a guest who did not sign in has no Zoom user ID")
}

func TestListPastMeetingParticipantsClassifiesZoomRefusals(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		code   string
	}{
		{name: "no ended instance", status: http.StatusNotFound, body: `{"code":3001,"message":"SENTINEL"}`, branch: zoom.ListPastMeetingParticipantsBranchNotFound, code: "3001"},
		{name: "free account", status: http.StatusBadRequest, body: `{"code":200,"message":"SENTINEL"}`, branch: zoom.ListPastMeetingParticipantsBranchProviderRejected, code: "200"},
		{name: "older than Zoom keeps", status: http.StatusBadRequest, body: `{"code":12702,"message":"SENTINEL"}`, branch: zoom.ListPastMeetingParticipantsBranchProviderRejected, code: "12702"},
		{name: "negative duration", status: http.StatusOK, body: `{"participants":[{"id":"a","duration":-1}]}`, branch: zoom.ListPastMeetingParticipantsBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newZoomDexContext("participants-"+test.name), newZoomClient(t, provider.URL).ListPastMeetingParticipants(), zoomConnection,
				zoom.ListPastMeetingParticipantsInput{MeetingID: 85746065})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, int64(85746065), result.Value.MeetingID)
			if test.code != "" {
				require.Equal(t, test.code, result.Receipt.Metadata["zoomErrorCode"])
			}
		})
	}
}

func TestListPastMeetingParticipantsRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {
		t.Fatal("invalid input must not reach Zoom")
	})
	client := newZoomClient(t, provider.URL)
	for _, input := range []zoom.ListPastMeetingParticipantsInput{{}, {MeetingID: 1, PageSize: 500}, {MeetingID: 1, PageToken: "bad token"}} {
		result, err := sdkgo.RunQuery(newZoomDexContext("participants-invalid"), client.ListPastMeetingParticipants(), zoomConnection, input)
		require.NoError(t, err)
		require.Equal(t, zoom.ListPastMeetingParticipantsBranchDefect, result.Branch)
	}
	require.Zero(t, provider.requestCount())
}

func timePointer(value time.Time) *time.Time { return &value }
