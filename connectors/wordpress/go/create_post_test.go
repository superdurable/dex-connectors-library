// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

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

const createdPostBody = `{"id":43,"status":"draft"}`

var testCreateInput = CreatePostInput{Title: "Meridian release", Content: "<p>Draft.</p>"}

// newScriptedServer answers each create with the next status and body, counting requests.
func newScriptedServer(t *testing.T, statuses []int, bodies []string) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, hasBasic := r.BasicAuth()
		require.True(t, hasBasic)
		require.Equal(t, "editor", username)
		require.Equal(t, "abcdefgh", password)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/wp-json/wp/v2/posts", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body struct {
			Title   string `json:"title"`
			Content string `json:"content"`
			Status  string `json:"status"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "Meridian release", body.Title)
		require.Equal(t, "<p>Draft.</p>", body.Content)
		require.Equal(t, "draft", body.Status)
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

func TestCreatePostNeverResendsConfirmedPost(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusCreated}, []string{createdPostBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("create-post-flow", "create-post-step")
	created, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchCreated, created.Branch)
	require.EqualValues(t, 43, created.Value.ID)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestCreatePostRetries429ThenCreates(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusTooManyRequests, http.StatusCreated}, []string{`{}`, createdPostBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("retry-flow", "create-post-step")
	_, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.Error(t, err)
	require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchCreated, created.Branch)
	require.Equal(t, 2, *requests)
}

func TestCreatePostResendsUnknownOutcomeBelowCap(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTeapot} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, requests := newScriptedServer(t, []int{status, http.StatusCreated}, []string{`{"code":"error"}`, createdPostBody})
			client := newTestClient(t, server.URL)
			ctx := singledispatchtest.NewContext("resend-flow", "create-post-step")
			_, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
			require.Error(t, err)
			require.Equal(t, singledispatch.StateClaimed, lastMarker(t, ctx).State)
			ctx.AdvanceAttempt()
			created, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
			require.NoError(t, err)
			require.Equal(t, CreatePostBranchCreated, created.Branch)
			require.EqualValues(t, 2, lastMarker(t, ctx).SendCount)
			require.Equal(t, 2, *requests)
		})
	}
}

func TestCreatePostResendsMalformedSuccessBelowCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusCreated, http.StatusCreated}, []string{`not JSON`, createdPostBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("malformed-flow", "create-post-step")
	_, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchCreated, created.Branch)
	require.Equal(t, 2, *requests)
}

func TestCreatePostSelectsUncertainAtCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError}, []string{`{}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("cap-flow", "create-post-step")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
		require.Error(t, err, "attempt %d resends", attempt)
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 3, *requests)
	ctx.AdvanceAttempt()
	refused, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, refused.Branch)
	require.Equal(t, 3, *requests)
}

func TestCreatePostHonorsApplicationSendCap(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusBadGateway}, []string{`{}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("override-flow", "create-post-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestCreatePostNeverResendsDefiniteRefusal(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, requests := newScriptedServer(t, []int{status}, []string{fmt.Sprintf(`{"code":"rest_error","data":{"status":%d}}`, status)})
			client := newTestClient(t, server.URL)
			ctx := singledispatchtest.NewContext("refusal-flow", "create-post-step")
			rejected, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
			require.NoError(t, err)
			require.Equal(t, CreatePostBranchProviderRejected, rejected.Branch)
			require.NotContains(t, rejected.Failure.Message, "abcdefgh")
			require.Equal(t, singledispatch.StateCanResend, lastMarker(t, ctx).State)
			require.Equal(t, 1, *requests)
		})
	}
}

func TestCreatePostRefusalOfResendSelectsUncertain(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError, http.StatusBadRequest}, []string{`{}`, `{"code":"rest_invalid_param"}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("refused-resend-flow", "create-post-step")
	_, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 2, *requests)
}

func TestCreatePostAcceptedButUnexpectedResponseIsNeverResent(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusOK}, []string{`{"id":43,"status":"trash"}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("unexpected-flow", "create-post-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, *requests)
}

func TestCreatePostFailClosedReturnsRetry(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusCreated}, []string{createdPostBody})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("fail-closed-flow", "create-post-step")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.Error(t, err)
	require.Equal(t, 0, *requests)
	ctx.SetReadError(nil)
	created, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchCreated, created.Branch)
}

func TestCreatePostCredentialFailureAfterPossibleSendRetries(t *testing.T) {
	server, requests := newScriptedServer(t, []int{http.StatusInternalServerError}, []string{`{}`})
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("credential-flow", "create-post-step")
	_, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, testCreateInput)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	noCredentials, err := New(Config{SiteURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, noCredentials.CreatePost(), testConnection, testCreateInput)
	require.Error(t, err)
	require.Equal(t, 1, *requests)
	defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("first-credential-flow", "create-post-step"), noCredentials.CreatePost(), testConnection, testCreateInput)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchDefect, defect.Branch)
}
