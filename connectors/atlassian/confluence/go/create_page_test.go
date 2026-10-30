// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validCreatePageInput() confluence.CreatePageInput {
	return confluence.CreatePageInput{SpaceID: testSpaceID, ParentPageID: "65537", Title: testPolicyTitle, Body: testPolicyBody}
}

// pageStore is a stateful fake of Confluence's one-page-per-title rule within a space.
type pageStore struct {
	t          *testing.T
	mutex      sync.Mutex
	pages      map[string]storedPage
	nextPageID int
}

type storedPage struct {
	id        string
	title     string
	parentID  string
	storage   string
	createdAt time.Time
}

func newPageStore(t *testing.T) *pageStore {
	return &pageStore{t: t, pages: map[string]storedPage{}, nextPageID: 600000}
}

func (store *pageStore) add(title string, parentID string, storage string, createdAt time.Time) storedPage {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.nextPageID++
	page := storedPage{id: strconv.Itoa(store.nextPageID), title: title, parentID: parentID, storage: storage, createdAt: createdAt}
	store.pages[title] = page
	return page
}

func (store *pageStore) count() int {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return len(store.pages)
}

// create applies a POST /pages body and reports false when the title is taken.
func (store *pageStore) create(request *http.Request) (storedPage, bool) {
	var body struct {
		SpaceID  string `json:"spaceId"`
		Title    string `json:"title"`
		ParentID string `json:"parentId"`
		Body     struct {
			Value string `json:"value"`
		} `json:"body"`
	}
	require.NoError(store.t, json.NewDecoder(request.Body).Decode(&body))
	store.mutex.Lock()
	_, isTaken := store.pages[body.Title]
	store.mutex.Unlock()
	if isTaken {
		return storedPage{}, false
	}
	return store.add(body.Title, body.ParentID, body.Body.Value, time.Now()), true
}

func (store *pageStore) lookupJSON(title string) string {
	store.mutex.Lock()
	page, isFound := store.pages[title]
	store.mutex.Unlock()
	if !isFound {
		return `{"results":[],"_links":{"base":"` + testSiteBase + `"}}`
	}
	return `{"results":[` + store.pageJSON(page) + `],"_links":{"base":"` + testSiteBase + `"}}`
}

func (store *pageStore) pageJSON(page storedPage) string {
	return pageJSONUnderParent(store.t, page.id, page.title, page.parentID, 1, "", page.createdAt, page.storage)
}

// serve answers the create, title lookup, and space lookup requests the createPage operation sends.
func (store *pageStore) serve(t *testing.T, response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == testContentPath+"/spaces":
		writeJSON(t, response, http.StatusOK, `{"results":[{"id":"`+testSpaceID+`","key":"OPS","name":"Operations"}]}`)
	case request.Method == http.MethodGet && request.URL.Path == testContentPath+"/pages":
		require.Equal(t, testSpaceID, request.URL.Query().Get("space-id"))
		require.Equal(t, "storage", request.URL.Query().Get("body-format"))
		writeJSON(t, response, http.StatusOK, store.lookupJSON(request.URL.Query().Get("title")))
	case request.Method == http.MethodPost && request.URL.Path == testContentPath+"/pages":
		page, isCreated := store.create(request)
		if !isCreated {
			writeJSON(t, response, http.StatusBadRequest, `{"errors":[{"status":400,"code":"BAD_REQUEST","title":"SENTINEL A page with this title already exists"}]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, store.pageJSON(page))
	default:
		t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
	}
}

func TestCreatePageSendsAStorageBodyAndReturnsThePage(t *testing.T) {
	store := newPageStore(t)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) { store.serve(t, response, request) })
	client := newConfluenceClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreatePage(), confluenceConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchCreated, result.Branch)
	require.Equal(t, confluence.CreatePageOutput{
		PageID: "600001", Title: testPolicyTitle, SpaceID: testSpaceID, ParentPageID: "65537", VersionNumber: 1,
		WebURL: testSiteBase + "/spaces/OPS/pages/600001",
	}, result.Value)
	require.Equal(t, "600001", result.Receipt.ProviderObjectID)
	require.Equal(t, 1, provider.requestCount(), "a confirmed create sends one request")
	require.JSONEq(t, `{"spaceId":"98306","status":"current","title":"Remote work policy","parentId":"65537",
		"body":{"representation":"storage","value":"<h1>Remote work</h1><p>Staff may work <strong>remotely</strong> two days a week.</p><ul><li>Ask your manager</li><li>Log the days</li></ul>"}}`,
		provider.request(0).body)
}

func TestCreatePageResolvesASpaceKeyFirst(t *testing.T) {
	store := newPageStore(t)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) { store.serve(t, response, request) })
	client := newConfluenceClient(t, provider.URL)
	input := validCreatePageInput()
	input.SpaceID, input.SpaceKey, input.BodyFormat = "", "OPS", confluence.TextFormatPlainText

	result, err := sdkgo.RunMutation(newTestDexContext("create-by-key"), client.CreatePage(), confluenceConnection, input)
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchCreated, result.Branch)
	require.Equal(t, "OPS", result.Value.SpaceKey)
	require.Equal(t, testSpaceID, result.Value.SpaceID)
	require.Equal(t, "keys=OPS&limit=1", provider.request(0).rawQuery)
	var body struct {
		Body struct {
			Value string `json:"value"`
		} `json:"body"`
	}
	require.NoError(t, json.Unmarshal([]byte(provider.request(1).body), &body))
	require.Equal(t, "<p># Remote work</p><p>Staff may work **remotely** two days a week.</p><p>- Ask your manager<br />- Log the days</p>", body.Body.Value,
		"plain text is never read as markup")
}

func TestCreatePageInAMissingSpaceSendsNoCreate(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"results":[]}`)
	})
	client := newConfluenceClient(t, provider.URL)
	input := validCreatePageInput()
	input.SpaceID, input.SpaceKey = "", "NOPE"
	result, err := sdkgo.RunMutation(newTestDexContext("create-no-space"), client.CreatePage(), confluenceConnection, input)
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchNotFound, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreatePageWithATakenTitleReportsTheExistingPage(t *testing.T) {
	store := newPageStore(t)
	existing := store.add(testPolicyTitle, "65537", "<p>Old policy</p>", time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC))
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) { store.serve(t, response, request) })
	client := newConfluenceClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newTestDexContext("create-taken"), client.CreatePage(), confluenceConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchTitleConflict, result.Branch)
	require.Equal(t, existing.id, result.Value.PageID)
	require.Equal(t, 1, result.Value.VersionNumber)
	require.False(t, result.Value.IsConfirmedByTitleLookup)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, 1, store.count())
	requireNoSentinel(t, result)
}

func TestCreatePageRejectionsNameTheStatusOnly(t *testing.T) {
	for _, test := range []struct {
		status   int
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
		requests int
	}{
		{http.StatusBadRequest, confluence.CreatePageBranchProviderRejected, sdkgo.FailureValidation, 2},
		{http.StatusForbidden, confluence.CreatePageBranchProviderRejected, sdkgo.FailureAuthorization, 1},
		{http.StatusRequestEntityTooLarge, confluence.CreatePageBranchProviderRejected, sdkgo.FailureProviderRejection, 1},
		{http.StatusNotFound, confluence.CreatePageBranchNotFound, sdkgo.FailureNotFound, 1},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					writeJSON(t, response, http.StatusOK, `{"results":[]}`)
					return
				}
				writeJSON(t, response, test.status, `{"errors":[{"title":"SENTINEL owner@example.com"}]}`)
			})
			client := newConfluenceClient(t, provider.URL)
			dexContext := newTestDexContext("create-rejected")
			result, err := sdkgo.RunMutation(dexContext, client.CreatePage(), confluenceConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, fmt.Sprintf("Confluence rejected the page with HTTP %d", test.status), result.Failure.Message)
			require.Equal(t, test.requests, provider.requestCount(), "only a 400 or 409 is checked against the title")
			require.Equal(t, testPolicyTitle, result.Value.Title)
			requireNoSentinel(t, result)
		})
	}
}

func TestCreateRefusedWithALookupRefusedIsARejectionAndALookupOutageRetries(t *testing.T) {
	for _, test := range []struct {
		lookupStatus int
		isRetry      bool
	}{
		{http.StatusForbidden, false},
		{http.StatusServiceUnavailable, true},
	} {
		provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			if request.Method == http.MethodGet {
				writeJSON(t, response, test.lookupStatus, `{"message":"SENTINEL lookup"}`)
				return
			}
			writeJSON(t, response, http.StatusBadRequest, `{"errors":[{"title":"SENTINEL invalid"}]}`)
		})
		result, err := sdkgo.RunMutation(newTestDexContext("create-lookup"), newConfluenceClient(t, provider.URL).CreatePage(), confluenceConnection, validCreatePageInput())
		if test.isRetry {
			requireRetry(t, err, sdkgo.FailureAvailability)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, confluence.CreatePageBranchProviderRejected, result.Branch)
		require.Equal(t, "Confluence rejected the page with HTTP 400", result.Failure.Message)
	}
}

func TestRateLimitedCreateRetriesAndClearsTheCheckpoint(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "3")
		writeJSON(t, response, http.StatusTooManyRequests, `{"message":"SENTINEL slow down"}`)
	})
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("create-rate-limited")
	_, err := sdkgo.RunMutation(dexContext, client.CreatePage(), confluenceConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 3*time.Second, retryAfter.After)
	require.False(t, dexContext.hasHeartbeat(), "a 429 created nothing, so the next attempt may send again")
}

func TestCreateTimeoutAfterConfluenceStoredThePageIsConfirmedByTitle(t *testing.T) {
	store := newPageStore(t)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			store.create(request)
			// Confluence stored the page, but the response never arrives before the connector gives up.
			<-request.Context().Done()
			return
		}
		store.serve(t, response, request)
	})
	client := newConfluenceClient(t, provider.URL, confluence.WithHTTPClient(&http.Client{Timeout: 300 * time.Millisecond}))

	result, err := sdkgo.RunMutation(newTestDexContext("create-timeout"), client.CreatePage(), confluenceConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchCreated, result.Branch)
	require.True(t, result.Value.IsConfirmedByTitleLookup)
	require.Equal(t, "600001", result.Value.PageID)
	require.Equal(t, 1, provider.countRequests(http.MethodPost, testContentPath+"/pages"))
}

func TestCreateOutageBeforeConfluenceStoredThePageIsSentAgainSafely(t *testing.T) {
	store := newPageStore(t)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if request.Method == http.MethodPost && index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"message":"SENTINEL unavailable"}`)
			return
		}
		store.serve(t, response, request)
	})
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("create-outage")

	_, err := sdkgo.RunMutation(dexContext, client.CreatePage(), confluenceConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 5*time.Second, retryAfter.After, "the next attempt waits for an in-flight create to settle")
	require.True(t, dexContext.hasHeartbeat(), "the checkpoint makes the next attempt look the title up first")

	result, err := sdkgo.RunMutation(dexContext, client.CreatePage(), confluenceConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchCreated, result.Branch)
	require.False(t, result.Value.IsConfirmedByTitleLookup)
	require.Equal(t, 1, store.count(), "Confluence keeps one page per title, so the second send created the only page")
	require.Equal(t, http.MethodGet, provider.request(2).method, "the retry looked the title up before sending")
}

func TestCreateAfterAnEarlierDispatchReportsThePageWithoutSending(t *testing.T) {
	store := newPageStore(t)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) { store.serve(t, response, request) })
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("create-earlier-dispatch")
	storage := "<h1>Remote work</h1><p>Staff may work <strong>remotely</strong> two days a week.</p><ul><li>Ask your manager</li><li>Log the days</li></ul>"
	page := store.add(testPolicyTitle, "65537", storage, time.Now())
	require.NoError(t, dexContext.RecordHeartbeat(map[string]any{"dispatchedAtUnixMilli": time.Now().Add(-10 * time.Second).UnixMilli()}))

	result, err := sdkgo.RunMutation(dexContext, client.CreatePage(), confluenceConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchCreated, result.Branch)
	require.True(t, result.Value.IsConfirmedByTitleLookup)
	require.Equal(t, page.id, result.Value.PageID)
	require.Zero(t, provider.countRequests(http.MethodPost, testContentPath+"/pages"))
}

func TestCreateFindsAnotherPageWithTheTitleAsAConflict(t *testing.T) {
	for _, test := range []struct {
		name      string
		parentID  string
		storage   string
		createdAt time.Time
	}{
		{"created before this Step", "65537", "<h1>Remote work</h1><p>Staff may work <strong>remotely</strong> two days a week.</p><ul><li>Ask your manager</li><li>Log the days</li></ul>", time.Now().Add(-time.Hour)},
		{"other content", "65537", "<p>Someone else's draft</p>", time.Now()},
		{"other parent", "70000", "<h1>Remote work</h1><p>Staff may work <strong>remotely</strong> two days a week.</p><ul><li>Ask your manager</li><li>Log the days</li></ul>", time.Now()},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newPageStore(t)
			provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) { store.serve(t, response, request) })
			client := newConfluenceClient(t, provider.URL)
			dexContext := newTestDexContext("create-conflict")
			store.add(testPolicyTitle, test.parentID, test.storage, test.createdAt)
			require.NoError(t, dexContext.RecordHeartbeat(map[string]any{"dispatchedAtUnixMilli": time.Now().UnixMilli()}))

			result, err := sdkgo.RunMutation(dexContext, client.CreatePage(), confluenceConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, confluence.CreatePageBranchTitleConflict, result.Branch)
			require.Zero(t, provider.countRequests(http.MethodPost, testContentPath+"/pages"))
		})
	}
}

func TestCreateWithAnUnusableSuccessBodyIsConfirmedByTitle(t *testing.T) {
	store := newPageStore(t)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			store.create(request)
			writeJSON(t, response, http.StatusOK, `{"id":"not-numeric"}`)
			return
		}
		store.serve(t, response, request)
	})
	client := newConfluenceClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newTestDexContext("create-unusable"), client.CreatePage(), confluenceConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.CreatePageBranchCreated, result.Branch)
	require.True(t, result.Value.IsConfirmedByTitleLookup)
}

func TestCreateWithoutARecordedCheckpointSendsNothing(t *testing.T) {
	provider := newRecordingConfluence(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("create-no-checkpoint")
	dexContext.recordErr = errors.New("worker stream closed")
	_, err := sdkgo.RunMutation(dexContext, client.CreatePage(), confluenceConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
}

func TestCreateThatNeverConnectedRetriesAndClearsTheCheckpoint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newConfluenceClient(t, "http://"+address)
	dexContext := newTestDexContext("create-refused")
	_, err = sdkgo.RunMutation(dexContext, client.CreatePage(), confluenceConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.False(t, dexContext.hasHeartbeat())
}

func TestCreatePageValidatesInputWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingConfluence(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newConfluenceClient(t, provider.URL)
	for _, test := range []struct {
		input   confluence.CreatePageInput
		message string
	}{
		{confluence.CreatePageInput{Title: "x", Body: "y"}, "set exactly one of spaceId and spaceKey"},
		{confluence.CreatePageInput{SpaceID: "1", SpaceKey: "OPS", Title: "x", Body: "y"}, "set exactly one of spaceId and spaceKey"},
		{confluence.CreatePageInput{SpaceKey: `OPS"`, Title: "x", Body: "y"}, "spaceKey must be a space key such as OPS"},
		{confluence.CreatePageInput{SpaceID: "1", ParentPageID: "abc", Title: "x", Body: "y"}, "parentPageId must be a numeric Confluence page ID"},
		{confluence.CreatePageInput{SpaceID: "1", Title: "two\nlines", Body: "y"}, "title must be one line without control characters"},
		{confluence.CreatePageInput{SpaceID: "1", Title: "x", Body: " "}, "body is required"},
	} {
		result, err := sdkgo.RunMutation(newTestDexContext("create-invalid"), client.CreatePage(), confluenceConnection, test.input)
		require.NoError(t, err)
		require.Equal(t, confluence.CreatePageBranchDefect, result.Branch)
		require.Equal(t, test.message, result.Failure.Message)
	}
}
