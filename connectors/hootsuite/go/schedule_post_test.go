// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var exampleSchedule = SchedulePostInput{Text: "A useful update", SocialProfileID: "115185509", ScheduledSendTime: "2026-11-15T14:00:00Z"}

func TestSchedulePostConfirmsOnce(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		var body struct {
			Text              string   `json:"text"`
			SocialProfileIDs  []string `json:"socialProfileIds"`
			ScheduledSendTime string   `json:"scheduledSendTime"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, exampleSchedule.Text, body.Text)
		require.Equal(t, []string{exampleSchedule.SocialProfileID}, body.SocialProfileIDs)
		require.Equal(t, exampleSchedule.ScheduledSendTime, body.ScheduledSendTime)
		_, err := w.Write([]byte(`{"data":[{"id":"4138476039","state":"SCHEDULED","socialProfile":{"id":"115185509"}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("schedule-post-flow", "schedule-post-step")
	created, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchScheduled, created.Branch)
	require.Equal(t, "4138476039", created.Value.ID)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
}

func TestSchedulePostRetries429ThenConfirms(t *testing.T) {
	status, requests := http.StatusTooManyRequests, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, err := w.Write([]byte(`{"data":[{"id":"4138476040","state":"SCHEDULED"}]}`))
			require.NoError(t, err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("schedule-rate-flow", "schedule-post-step")
	_, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.Error(t, err)
	status = http.StatusOK
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchScheduled, created.Branch)
	require.Equal(t, 2, requests)
}

func TestSchedulePostDoesNotRepeatAmbiguousServerError(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("schedule-uncertain-flow", "schedule-post-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchUncertain, uncertain.Branch)
	ctx.AdvanceAttempt()
	repeated, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchUncertain, repeated.Branch)
	require.Equal(t, 1, requests)
}
