// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var exampleSchedule = SchedulePostInput{Text: "A useful update", SocialProfileID: "115185509", ScheduledSendTime: "2026-11-15T14:00:00Z"}

const scheduledBody = `{"data":[{"id":"4138476039","state":"SCHEDULED","socialProfile":{"id":"115185509"}}]}`

// newScriptedServer answers each schedule request with the next status and body, counting requests.
func newScriptedServer(t *testing.T, statuses []int, bodies []string) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		index := min(requests, len(statuses)-1)
		requests++
		w.WriteHeader(statuses[index])
		_, err := fmt.Fprint(w, bodies[index])
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func lastMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &marker))
	return marker
}

func TestSchedulePostNeverResendsConfirmedMessage(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK}, []string{scheduledBody})
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
	require.Equal(t, 1, *requests)
}

func TestSchedulePostRetries429WithCheckpoint(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusTooManyRequests, http.StatusOK}, []string{`{}`, scheduledBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("retry-flow", "schedule-post-step")
	_, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.Error(t, err)
	require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchScheduled, created.Branch)
	require.Equal(t, 2, *requests)
}

func TestSchedulePostResendsUnknownOutcomeBelowCap(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTeapot} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, requests := newScriptedServer(t, []int{status, http.StatusOK}, []string{`{"errors":[]}`, scheduledBody})
			client := newTestClient(t, server.URL)
			ctx := singledispatchtest.NewContext("resend-flow", "schedule-post-step")
			_, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
			require.Error(t, err)
			require.Equal(t, singledispatch.StateClaimed, lastMarker(t, ctx).State)
			ctx.AdvanceAttempt()
			created, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
			require.NoError(t, err)
			require.Equal(t, SchedulePostBranchScheduled, created.Branch)
			require.EqualValues(t, 2, lastMarker(t, ctx).SendCount)
			require.Equal(t, 2, *requests)
		})
	}
}

func TestSchedulePostResendsMalformedSuccessBelowCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK, http.StatusOK}, []string{`not JSON`, scheduledBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("malformed-flow", "schedule-post-step")
	_, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchScheduled, created.Branch)
	require.Equal(t, 2, *requests)
}

func TestSchedulePostSelectsUncertainAtCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError}, []string{`{}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("cap-flow", "schedule-post-step")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
		require.Error(t, err, "attempt %d resends", attempt)
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 3, *requests)
	ctx.AdvanceAttempt()
	refused, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchUncertain, refused.Branch)
	require.Equal(t, 3, *requests)
}

func TestSchedulePostHonorsApplicationSendCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusBadGateway}, []string{`{}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("override-flow", "schedule-post-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSchedulePostNeverResendsDefiniteRefusal(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, requests := newScriptedServer(t, []int{status}, []string{`{"errors":[{"code":1000,"message":"rejected"}]}`})
			client := newTestClient(t, server.URL)
			ctx := singledispatchtest.NewContext("refusal-flow", "schedule-post-step")
			rejected, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
			require.NoError(t, err)
			require.Equal(t, SchedulePostBranchProviderRejected, rejected.Branch)
			require.NotContains(t, rejected.Failure.Message, testAccessToken)
			require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
			require.Equal(t, 1, *requests)
		})
	}
}

func TestSchedulePostRefusalOfResendSelectsUncertain(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError, http.StatusBadRequest}, []string{`{}`, `{"errors":[{"code":1000}]}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("refused-resend-flow", "schedule-post-step")
	_, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 2, *requests)
}

func TestSchedulePostAcceptedButIncompleteResponseIsNeverResent(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK}, []string{`{"data":[{"id":"4138476039"}]}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("incomplete-flow", "schedule-post-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestSchedulePostFailClosedReturnsRetry(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK}, []string{scheduledBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("fail-closed-flow", "schedule-post-step")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.Error(t, err)
	require.Equal(t, 0, *requests)
	ctx.SetReadError(nil)
	created, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchScheduled, created.Branch)
}

func TestSchedulePostInvalidCredentialAfterPossibleSendRetries(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError}, []string{`{}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("credential-flow", "schedule-post-step")
	_, err := sdkgo.RunMutation(ctx, client.SchedulePost(), testConnection, exampleSchedule)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	invalid := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("bad\ntoken"), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	invalidClient, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: invalid})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, invalidClient.SchedulePost(), testConnection, exampleSchedule)
	require.Error(t, err)
	require.Equal(t, 1, *requests)
	defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("first-credential-flow", "schedule-post-step"), invalidClient.SchedulePost(), testConnection, exampleSchedule)
	require.NoError(t, err)
	require.Equal(t, SchedulePostBranchDefect, defect.Branch)
}
