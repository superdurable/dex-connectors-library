// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetMeetingReturnsTheSafeMeetingView(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, meetingJSON(85746065, "Planning", "2036-02-24T17:00:00Z"))
	})
	result, err := sdkgo.RunQuery(newZoomDexContext("get"), newZoomClient(t, provider.URL).GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchFound, result.Branch)
	meeting := result.Value
	require.Equal(t, "Planning", meeting.Topic)
	require.Equal(t, "host@example.com", meeting.HostEmail)
	require.Equal(t, "waiting", meeting.Status)
	require.Equal(t, 2, meeting.Type)
	require.Equal(t, "Kickoff", meeting.Agenda)
	require.True(t, meeting.StartTime.Equal(futureStart))
	require.Equal(t, "85746065", result.Receipt.ProviderObjectID)
	encoded, err := json.Marshal(meeting)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), "start_url")
}

func TestGetMeetingClassifiesMissingAndMismatchedMeetings(t *testing.T) {
	missing := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"code":3001,"message":"Meeting does not exist: 85746065."}`)
	})
	result, err := sdkgo.RunQuery(newZoomDexContext("get-missing"), newZoomClient(t, missing.URL).GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchNotFound, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.Equal(t, "3001", result.Receipt.Metadata["zoomErrorCode"])
	require.Equal(t, int64(85746065), result.Value.ID)

	mismatched := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, meetingJSON(11111111, "Other", "2036-02-24T17:00:00Z"))
	})
	result, err = sdkgo.RunQuery(newZoomDexContext("get-mismatch"), newZoomClient(t, mismatched.URL).GetMeeting(), zoomConnection, zoom.GetMeetingInput{MeetingID: 85746065})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchInvalidResponse, result.Branch)

	invalid := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {
		t.Fatal("an invalid meeting ID must not reach Zoom")
	})
	result, err = sdkgo.RunQuery(newZoomDexContext("get-invalid"), newZoomClient(t, invalid.URL).GetMeeting(), zoomConnection, zoom.GetMeetingInput{})
	require.NoError(t, err)
	require.Equal(t, zoom.GetMeetingBranchDefect, result.Branch)
}
