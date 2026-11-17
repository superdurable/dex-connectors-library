// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookpages

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

const publishedPostBody = `{"id":"123456789_987654321"}`

var testPublishInput = PublishTextPostInput{PageID: "123456789", Message: "Hello from Dex"}

// newScriptedPublishServer answers each publish with the next status and body, counting requests.
func newScriptedPublishServer(t *testing.T, statuses []int, bodies []string) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/123456789/feed", r.URL.Path)
		require.Equal(t, "Bearer "+testPageToken, r.Header.Get("Authorization"))
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "Hello from Dex", r.PostForm.Get("message"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		index := min(requests, len(statuses)-1)
		requests++
		w.WriteHeader(statuses[index])
		_, err := fmt.Fprint(w, bodies[index])
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func lastPublishMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &marker))
	return marker
}

func TestPublishTextPostCompletesFromCheckpointedPostID(t *testing.T) {
	server, requests := newScriptedPublishServer(t, []int{http.StatusOK}, []string{publishedPostBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-publish", "publish-post")
	published, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchPublished, published.Branch)
	require.Equal(t, "123456789_987654321", published.Value.PostID)
	ctx.AdvanceAttempt()
	again, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchPublished, again.Branch)
	require.Equal(t, "123456789_987654321", again.Value.PostID)
	require.Equal(t, 1, *requests)
}

func TestPublishTextPostRetriesDefinitiveRateLimitAndUnavailable(t *testing.T) {
	for _, response := range []struct {
		status int
		body   string
	}{{http.StatusTooManyRequests, ``}, {http.StatusServiceUnavailable, ``}, {http.StatusBadRequest, `{"error":{"code":613}}`}} {
		t.Run(http.StatusText(response.status), func(t *testing.T) {
			server, requests := newScriptedPublishServer(t, []int{response.status, http.StatusOK}, []string{response.body, publishedPostBody})
			ctx := singledispatchtest.NewContext("facebook-pages-retry", "publish-post")
			client := newTestClient(t, server.URL)
			_, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, singledispatch.StateCanResend, lastPublishMarker(t, ctx).State)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
			require.NoError(t, err)
			require.Equal(t, PublishTextPostBranchPublished, result.Branch)
			require.Equal(t, 2, *requests)
		})
	}
}

func TestPublishTextPostResendsUnknownOutcomeBelowCap(t *testing.T) {
	for _, response := range []struct {
		status int
		body   string
	}{{500, ``}, {502, ``}, {408, ``}, {409, ``}, {425, ``}, {418, ``}, {200, `{}`}, {200, `not JSON`}} {
		t.Run(fmt.Sprintf("%d %s", response.status, response.body), func(t *testing.T) {
			server, requests := newScriptedPublishServer(t, []int{response.status, http.StatusOK}, []string{response.body, publishedPostBody})
			client := newTestClient(t, server.URL)
			ctx := singledispatchtest.NewContext("facebook-pages-resend", "publish-post")
			_, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
			require.Error(t, err)
			require.Equal(t, singledispatch.StateClaimed, lastPublishMarker(t, ctx).State)
			ctx.AdvanceAttempt()
			published, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
			require.NoError(t, err)
			require.Equal(t, PublishTextPostBranchPublished, published.Branch)
			require.EqualValues(t, 2, lastPublishMarker(t, ctx).SendCount)
			require.Equal(t, 2, *requests)
		})
	}
}

func TestPublishTextPostSelectsUncertainAtCap(t *testing.T) {
	server, requests := newScriptedPublishServer(t, []int{http.StatusInternalServerError}, []string{``})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-cap", "publish-post")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
		require.Error(t, err, "attempt %d resends", attempt)
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchUncertain, uncertain.Branch)
	ctx.AdvanceAttempt()
	refused, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchUncertain, refused.Branch)
	require.Equal(t, 3, *requests)
}

func TestPublishTextPostHonorsApplicationSendCap(t *testing.T) {
	server, requests := newScriptedPublishServer(t, []int{http.StatusBadGateway}, []string{``})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-override", "publish-post")
	uncertain, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestPublishTextPostInterruptedConnectionResends(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.NoError(t, r.ParseForm())
		if requests > 1 {
			_, err := fmt.Fprint(w, publishedPostBody)
			require.NoError(t, err)
			return
		}
		connection, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-interrupted", "publish-post")
	_, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	published, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchPublished, published.Branch)
	require.Equal(t, 2, requests)
}

func TestPublishTextPostNeverResendsDefiniteRefusal(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, requests := newScriptedPublishServer(t, []int{status}, []string{`{"error":{"code":100,"message":"private user data"}}`})
			client := newTestClient(t, server.URL)
			ctx := singledispatchtest.NewContext("facebook-pages-reject", "publish-post")
			rejected, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
			require.NoError(t, err)
			require.Equal(t, PublishTextPostBranchProviderRejected, rejected.Branch)
			require.NotContains(t, rejected.Failure.Message, "private user data")
			require.NotContains(t, rejected.Failure.Message, testPageToken)
			require.Equal(t, singledispatch.StateCanResend, lastPublishMarker(t, ctx).State)
			require.Equal(t, 1, *requests)
		})
	}
}

func TestPublishTextPostRefusalOfResendSelectsUncertain(t *testing.T) {
	server, requests := newScriptedPublishServer(t, []int{http.StatusInternalServerError, http.StatusBadRequest}, []string{``, `{"error":{"code":100}}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-refused-resend", "publish-post")
	_, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchUncertain, uncertain.Branch)
	require.Equal(t, 2, *requests)
}

func TestPublishTextPostFailClosedReturnsRetry(t *testing.T) {
	server, requests := newScriptedPublishServer(t, []int{http.StatusOK}, []string{publishedPostBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-fail-closed", "publish-post")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.Error(t, err)
	require.Equal(t, 0, *requests)
	ctx.SetReadError(nil)
	published, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchPublished, published.Branch)
}

func TestPublishTextPostCredentialFailureAfterPossibleSendRetries(t *testing.T) {
	server, requests := newScriptedPublishServer(t, []int{http.StatusInternalServerError}, []string{``})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-credentials", "publish-post")
	_, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, testPublishInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	noCredentials, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, noCredentials.PublishTextPost(), testConnection, testPublishInput)
	require.Error(t, err)
	require.Equal(t, 1, *requests)
	defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("facebook-pages-first-credentials", "publish-post"), noCredentials.PublishTextPost(), testConnection, testPublishInput)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchDefect, defect.Branch)
}

func TestPublishTextPostRejectsInvalidInput(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:1")
	ctx := singledispatchtest.NewContext("facebook-pages-invalid", "publish-post")
	invalid, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, PublishTextPostInput{PageID: "bad/id", Message: "Hello"})
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchDefect, invalid.Branch)
}
