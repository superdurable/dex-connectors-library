// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const testCommentStorage = "<p>Published for the <strong>Q4</strong> review.</p>"

func validAddCommentInput() confluence.AddCommentInput {
	return confluence.AddCommentInput{PageID: testPolicyPageID, Body: "Published for the **Q4** review."}
}

func footerCommentJSON(t *testing.T, id string, storage string, createdAt time.Time) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"id": id, "status": "current", "pageId": testPolicyPageID,
		"version": map[string]any{"number": 1, "createdAt": createdAt.UTC().Format("2006-01-02T15:04:05.000Z")},
		"body":    map[string]any{"storage": map[string]any{"representation": "storage", "value": storage}},
	})
	require.NoError(t, err)
	return string(encoded)
}

func TestAddCommentSendsAFooterCommentOnce(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, footerCommentJSON(t, "720897", testCommentStorage, time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)))
	})
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("comment")

	result, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchAdded, result.Branch)
	require.Equal(t, confluence.AddCommentOutput{
		PageID: testPolicyPageID, CommentID: "720897", CreatedAt: time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC),
	}, result.Value)
	request := provider.request(0)
	require.Equal(t, testContentPath+"/footer-comments", request.path)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(request.body), &body))
	require.Equal(t, map[string]any{"pageId": testPolicyPageID, "body": map[string]any{"representation": "storage", "value": testCommentStorage}}, body)
	require.True(t, dexContext.hasHeartbeat(), "the dispatch checkpoint was recorded before sending")
}

func TestUnconfirmedCommentIsNeverSentAgain(t *testing.T) {
	var mutex sync.Mutex
	comments := `{"results":[]}`
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			// Confluence stored the comment, but the response never arrives before the connector gives up.
			mutex.Lock()
			comments = `{"results":[` + footerCommentJSON(t, "720898", "<p>Older note</p>", time.Now()) + `,` +
				footerCommentJSON(t, "720899", `<p local-id="x">Published for the <strong>Q4</strong>   review.</p>`, time.Now()) + `]}`
			mutex.Unlock()
			<-request.Context().Done()
			return
		}
		require.Equal(t, testContentPath+"/pages/"+testPolicyPageID+"/footer-comments", request.URL.Path)
		require.Equal(t, "body-format=storage&limit=25&sort=-created-date", request.URL.RawQuery)
		mutex.Lock()
		body := comments
		mutex.Unlock()
		writeJSON(t, response, http.StatusOK, body)
	})
	client := newConfluenceClient(t, provider.URL, confluence.WithHTTPClient(&http.Client{Timeout: 300 * time.Millisecond}))
	dexContext := newTestDexContext("comment-timeout")

	_, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	result, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchAdded, result.Branch)
	require.True(t, result.Value.IsConfirmedByReadBack)
	require.Equal(t, "720899", result.Value.CommentID)
	require.Equal(t, 1, provider.countRequests(http.MethodPost, testContentPath+"/footer-comments"))
}

func TestCommentAfterAnEarlierDispatchWithoutAMatchIsUncertain(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, http.MethodGet, request.Method, "a reconciling attempt never sends")
		writeJSON(t, response, http.StatusOK, `{"results":[`+footerCommentJSON(t, "720898", testCommentStorage, time.Now().Add(-time.Hour))+`]}`)
	})
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("comment-uncertain")
	require.NoError(t, dexContext.RecordHeartbeat(map[string]any{"dispatchedAtUnixMilli": time.Now().UnixMilli()}))

	result, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchUncertain, result.Branch, "the same text written an hour ago is another comment")
	require.Empty(t, result.Value.CommentID)
	require.Equal(t, testPolicyPageID, result.Value.PageID)
}

func TestCommentRejectionsAndRateLimits(t *testing.T) {
	for _, test := range []struct {
		status int
		branch sdkgo.BranchID
	}{
		{http.StatusNotFound, confluence.AddCommentBranchNotFound},
		{http.StatusBadRequest, confluence.AddCommentBranchProviderRejected},
		{http.StatusForbidden, confluence.AddCommentBranchProviderRejected},
	} {
		provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, test.status, `{"errors":[{"title":"SENTINEL nope"}]}`)
		})
		result, err := sdkgo.RunMutation(newTestDexContext("comment-rejected"), newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
		require.NoError(t, err)
		require.Equal(t, test.branch, result.Branch)
		requireNoSentinel(t, result)
	}
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusTooManyRequests, `{"message":"SENTINEL slow down"}`)
	})
	dexContext := newTestDexContext("comment-rate-limited")
	_, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	require.False(t, dexContext.hasHeartbeat(), "a 429 added nothing, so the next attempt may send")
}

func TestCommentWithoutARecordedCheckpointSendsNothing(t *testing.T) {
	provider := newRecordingConfluence(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	dexContext := newTestDexContext("comment-no-checkpoint")
	dexContext.recordErr = errors.New("worker stream closed")
	_, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
}
