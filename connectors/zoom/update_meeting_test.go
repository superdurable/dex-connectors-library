// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateMeetingPatchesOnlyTheRequestedFields(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.WriteHeader(http.StatusNoContent)
	})
	client := newZoomClient(t, provider.URL)
	clearedAgenda, isMutedOnEntry := "", true

	result, err := sdkgo.RunMutation(newZoomDexContext("update"), client.UpdateMeeting(), zoomConnection, zoom.UpdateMeetingInput{
		MeetingID: 85746065, StartTime: "2036-02-25T10:30:00+01:00", TimeZone: "Europe/Berlin", DurationMinutes: 45,
		Agenda: &clearedAgenda, Settings: &zoom.MeetingSettingsInput{MuteUponEntry: &isMutedOnEntry},
	})
	require.NoError(t, err)
	require.Equal(t, zoom.UpdateMeetingBranchUpdated, result.Branch)
	require.Equal(t, zoom.UpdatedMeeting{MeetingID: 85746065}, result.Value)
	require.Equal(t, "85746065", result.Receipt.ProviderObjectID)
	request := provider.request(t, 0)
	require.Equal(t, http.MethodPatch, request.method)
	require.Equal(t, "/meetings/85746065", request.path)
	require.Equal(t, map[string]any{
		"start_time": "2036-02-25T09:30:00Z", "timezone": "Europe/Berlin", "duration": float64(45), "agenda": "",
		"settings": map[string]any{"mute_upon_entry": true},
	}, decodeRequestBody(t, request))
}

func TestUpdateMeetingRejectsEmptyAndAmbiguousPatchesWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {
		t.Fatal("an invalid patch must not reach Zoom")
	})
	client := newZoomClient(t, provider.URL)
	for _, test := range []struct {
		name   string
		input  zoom.UpdateMeetingInput
		reason string
	}{
		{name: "no change", input: zoom.UpdateMeetingInput{MeetingID: 85746065}, reason: "at least one change"},
		{name: "empty settings", input: zoom.UpdateMeetingInput{MeetingID: 85746065, Settings: &zoom.MeetingSettingsInput{}}, reason: "at least one change"},
		{name: "zone without start", input: zoom.UpdateMeetingInput{MeetingID: 85746065, TimeZone: "Europe/Berlin"}, reason: "timeZone requires startTime"},
		{name: "start without zone", input: zoom.UpdateMeetingInput{MeetingID: 85746065, StartTime: "2036-02-25T10:30:00+01:00"}, reason: "timeZone is required"},
		{name: "past start", input: zoom.UpdateMeetingInput{MeetingID: 85746065, StartTime: "2020-02-25T10:30:00+01:00", TimeZone: "Europe/Berlin"}, reason: "not in the future"},
		{name: "missing meeting", input: zoom.UpdateMeetingInput{Topic: "Renamed"}, reason: "meetingId"},
		{name: "negative duration", input: zoom.UpdateMeetingInput{MeetingID: 85746065, DurationMinutes: -5}, reason: "between 1 and 1440"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newZoomDexContext("update-"+test.name), client.UpdateMeeting(), zoomConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, zoom.UpdateMeetingBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.reason)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestUpdateMeetingRetriesUnknownOutcomesBecauseThePatchIsAbsolute(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadGateway, `{"code":502,"message":"SENTINEL"}`)
	})
	_, err := sdkgo.RunMutation(newZoomDexContext("update-unavailable"), newZoomClient(t, provider.URL).UpdateMeeting(), zoomConnection, zoom.UpdateMeetingInput{MeetingID: 85746065, Topic: "Renamed"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)

	provider.Close()
	_, err = sdkgo.RunMutation(newZoomDexContext("update-unreachable"), newZoomClient(t, provider.URL).UpdateMeeting(), zoomConnection, zoom.UpdateMeetingInput{MeetingID: 85746065, Topic: "Renamed"})
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestUpdateMeetingClassifiesMissingMeetingsAndDailyLimits(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		body       string
		branch     sdkgo.BranchID
	}{
		{name: "deleted meeting", status: http.StatusNotFound, body: `{"code":3001,"message":"SENTINEL"}`, branch: zoom.UpdateMeetingBranchNotFound},
		{name: "daily update limit", status: http.StatusTooManyRequests, retryAfter: "7200", body: `{"code":429}`, branch: zoom.UpdateMeetingBranchProviderRejected},
		{name: "invalid schedule", status: http.StatusBadRequest, body: `{"code":300,"message":"SENTINEL"}`, branch: zoom.UpdateMeetingBranchProviderRejected},
		{name: "redirect", status: http.StatusTemporaryRedirect, body: `{}`, branch: zoom.UpdateMeetingBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newZoomDexContext("update-"+test.name), newZoomClient(t, provider.URL).UpdateMeeting(), zoomConnection, zoom.UpdateMeetingInput{MeetingID: 85746065, Topic: "Renamed"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, int64(85746065), result.Value.MeetingID)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}
