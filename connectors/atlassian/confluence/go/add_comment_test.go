// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
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
	// Without a checkpoint, each earlier attempt counts as a possible send, so this attempt is at the cap.
	dexContext.attempt = confluence.AddCommentDefinition.MaxSends + 1

	result, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchUncertain, result.Branch, "the same text written an hour ago is another comment")
	require.Empty(t, result.Value.CommentID)
	require.Equal(t, testPolicyPageID, result.Value.PageID)
}

func TestCommentReadsBackThenResendsBelowTheSendCap(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"results":[]}`)
			return
		}
		writeJSON(t, response, http.StatusInternalServerError, `{}`)
	})
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("comment-resend")
	for attempt := int32(1); attempt < confluence.AddCommentDefinition.MaxSends; attempt++ {
		dexContext.attempt = attempt
		_, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
		requireRetry(t, err, sdkgo.FailureAvailability)
		require.Equal(t, int(attempt), provider.countRequests(http.MethodPost, testContentPath+"/footer-comments"))
	}
	require.Equal(t, 1, provider.countRequests(http.MethodGet, testContentPath+"/pages/"+testPolicyPageID+"/footer-comments"), "the resend read back first")
	dexContext.attempt = confluence.AddCommentDefinition.MaxSends + 1
	result, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, int(confluence.AddCommentDefinition.MaxSends)-1, provider.countRequests(http.MethodPost, testContentPath+"/footer-comments"))
}

func TestCommentRefusalOfAResendIsUncertain(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"results":[]}`)
			return
		}
		writeJSON(t, response, http.StatusBadRequest, `{"errors":[{"title":"SENTINEL nope"}]}`)
	})
	dexContext := newTestDexContext("comment-resend-refused")
	dexContext.attempt = 2
	result, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchUncertain, result.Branch, "the earlier attempt may have added the comment")
	require.Equal(t, 1, provider.countRequests(http.MethodPost, testContentPath+"/footer-comments"))
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
	require.Contains(t, string(dexContext.heartbeat), `"state":"canResend"`, "a 429 added nothing, so the next attempt may send")
}

func TestCommentRefusalWithAnUnreadableBodyIsUnknown(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"interrupted 400": func(response http.ResponseWriter) {
			response.Header().Set("Content-Length", "100")
			response.WriteHeader(http.StatusBadRequest)
			_, _ = response.Write([]byte(`{"errors"`))
		},
		"oversized 404": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusNotFound, `{"message":"`+strings.Repeat("x", 70<<10)+`"}`)
		},
	} {
		provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) { reply(response) })
		dexContext := newTestDexContext("comment-unreadable-refusal")
		_, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
		requireRetry(t, err, sdkgo.FailureTransport)
		require.Contains(t, string(dexContext.heartbeat), `"state":"claimed"`, name+": an unknown outcome keeps the claim")
	}
}

func TestCommentCredentialFailureAfterAPossibleSend(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadGateway, `{"message":"SENTINEL"}`)
	})
	dexContext := newTestDexContext("comment-credential-after-send")
	_, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
	require.Error(t, err)
	claim := string(dexContext.heartbeat)
	dexContext.attempt = 2
	brokerDown, err := confluence.New(confluence.Config{CloudID: testCloudID, Endpoint: provider.URL}, failingConfluenceCredentials{projectconfig.ErrCredentialBrokerUnavailable})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(dexContext, brokerDown.AddComment(), confluenceConnection, validAddCommentInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, claim, string(dexContext.heartbeat), "notSent would hide the earlier possible send")
	reauthorize, err := confluence.New(confluence.Config{CloudID: testCloudID, Endpoint: provider.URL}, failingConfluenceCredentials{sdkgo.ErrReauthorizationRequired})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(dexContext, reauthorize.AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCommentServiceUnavailableRecordsCanResend(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"message":"SENTINEL unavailable"}`)
	})
	dexContext := newTestDexContext("comment-service-unavailable")
	_, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, string(dexContext.heartbeat), `"state":"canResend"`, "a 503 added nothing, so it is not counted as a send")
}

func TestCommentReadsBackAfterARateLimitedResend(t *testing.T) {
	var mutex sync.Mutex
	posts, reads := 0, 0
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		mutex.Lock()
		defer mutex.Unlock()
		if request.Method == http.MethodPost {
			posts++
			if posts == 1 {
				writeJSON(t, response, http.StatusInternalServerError, `{}`)
				return
			}
			writeJSON(t, response, http.StatusTooManyRequests, `{}`)
			return
		}
		reads++
		if reads == 1 {
			writeJSON(t, response, http.StatusOK, `{"results":[]}`)
			return
		}
		// The first send's comment becomes visible only on the third attempt's read-back.
		writeJSON(t, response, http.StatusOK, `{"results":[`+footerCommentJSON(t, "720900", testCommentStorage, time.Now())+`]}`)
	})
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("comment-rate-limited-resend")
	_, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	dexContext.attempt = 2
	_, err = sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	dexContext.attempt = 3
	result, err := sdkgo.RunMutation(dexContext, client.AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchAdded, result.Branch)
	require.Equal(t, "720900", result.Value.CommentID)
	require.Equal(t, 2, provider.countRequests(http.MethodPost, testContentPath+"/footer-comments"), "the third attempt read back instead of resending")
}

func TestCommentReadBackOfAMissingPageAfterAPossibleSendIsUncertain(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, http.MethodGet, request.Method, "a missing page is never resent to")
		writeJSON(t, response, http.StatusNotFound, `{"message":"SENTINEL missing"}`)
	})
	dexContext := newTestDexContext("comment-read-back-missing-page")
	dexContext.attempt = 2
	result, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
	require.NoError(t, err)
	require.Equal(t, confluence.AddCommentBranchUncertain, result.Branch)
	requireNoSentinel(t, result)
}

func TestCommentAcceptedWithABrokenBodyIsNeverResent(t *testing.T) {
	for _, test := range []struct {
		name         string
		visible      bool
		expectBranch sdkgo.BranchID
	}{
		{"confirmed by read-back", true, confluence.AddCommentBranchAdded},
		{"not confirmed", false, confluence.AddCommentBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					// The headers promise more bytes than arrive, so the body read fails after a 201.
					response.Header().Set("Content-Length", "4096")
					response.WriteHeader(http.StatusCreated)
					_, _ = response.Write([]byte(`{"id":"72`))
					return
				}
				comments := `{"results":[]}`
				if test.visible {
					comments = `{"results":[` + footerCommentJSON(t, "720901", testCommentStorage, time.Now()) + `]}`
				}
				writeJSON(t, response, http.StatusOK, comments)
			})
			dexContext := newTestDexContext("comment-broken-body")
			result, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
			require.NoError(t, err)
			require.Equal(t, test.expectBranch, result.Branch)
			require.Equal(t, 1, provider.countRequests(http.MethodPost, testContentPath+"/footer-comments"))
		})
	}
}

func TestCommentWithoutARecordedCheckpointSendsNothing(t *testing.T) {
	provider := newRecordingConfluence(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	dexContext := newTestDexContext("comment-no-checkpoint")
	dexContext.recordErr = errors.New("worker stream closed")
	_, err := sdkgo.RunMutation(dexContext, newConfluenceClient(t, provider.URL).AddComment(), confluenceConnection, validAddCommentInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
}
