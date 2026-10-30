// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validUpdatePageInput() confluence.UpdatePageInput {
	return confluence.UpdatePageInput{
		PageID: testPolicyPageID, Title: testPolicyTitle, Body: "Staff may work remotely **three** days a week.",
		NextVersionNumber: 4, VersionMessage: "Annual review",
	}
}

// versionedPage is a stateful fake of Confluence's rule that each version number is accepted once.
type versionedPage struct {
	t        *testing.T
	mutex    sync.Mutex
	versions map[int]string
	current  int
	puts     int
}

func newVersionedPage(t *testing.T, current int) *versionedPage {
	return &versionedPage{t: t, versions: map[int]string{current: "Earlier edit"}, current: current}
}

func (page *versionedPage) put(request *http.Request) (int, bool) {
	var body struct {
		Version struct {
			Number  int    `json:"number"`
			Message string `json:"message"`
		} `json:"version"`
	}
	require.NoError(page.t, json.NewDecoder(request.Body).Decode(&body))
	page.mutex.Lock()
	defer page.mutex.Unlock()
	page.puts++
	if body.Version.Number != page.current+1 {
		return page.current, false
	}
	page.current = body.Version.Number
	page.versions[page.current] = body.Version.Message
	return page.current, true
}

func (page *versionedPage) currentVersion() int {
	page.mutex.Lock()
	defer page.mutex.Unlock()
	return page.current
}

func (page *versionedPage) putCount() int {
	page.mutex.Lock()
	defer page.mutex.Unlock()
	return page.puts
}

func (page *versionedPage) editByAnotherUser() {
	page.mutex.Lock()
	defer page.mutex.Unlock()
	page.current++
	page.versions[page.current] = "Edited in the browser"
}

func (page *versionedPage) currentJSON() string {
	page.mutex.Lock()
	defer page.mutex.Unlock()
	return pageJSON(page.t, testPolicyPageID, testPolicyTitle, page.current, page.versions[page.current], time.Now(), "<p>x</p>")
}

func (page *versionedPage) versionJSON(number int) (string, bool) {
	page.mutex.Lock()
	defer page.mutex.Unlock()
	message, isFound := page.versions[number]
	encoded, err := json.Marshal(map[string]any{"number": number, "message": message, "createdAt": "2026-09-30T10:00:00.000Z"})
	require.NoError(page.t, err)
	return string(encoded), isFound
}

func (page *versionedPage) serve(t *testing.T, response http.ResponseWriter, request *http.Request) {
	versionPrefix := testContentPath + "/pages/" + testPolicyPageID + "/versions/"
	switch {
	case request.Method == http.MethodPut:
		if _, isApplied := page.put(request); !isApplied {
			writeJSON(t, response, http.StatusConflict, `{"errors":[{"status":409,"title":"SENTINEL Version must be incremented"}]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, page.currentJSON())
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, versionPrefix):
		var number int
		_, err := fmt.Sscanf(strings.TrimPrefix(request.URL.Path, versionPrefix), "%d", &number)
		require.NoError(t, err)
		body, isFound := page.versionJSON(number)
		if !isFound {
			writeJSON(t, response, http.StatusNotFound, `{}`)
			return
		}
		writeJSON(t, response, http.StatusOK, body)
	case request.Method == http.MethodGet:
		writeJSON(t, response, http.StatusOK, page.currentJSON())
	}
}

func TestUpdatePageSendsTheNextVersionWithAMarkedMessage(t *testing.T) {
	page := newVersionedPage(t, 3)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) { page.serve(t, response, request) })
	client := newConfluenceClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newTestDexContext("update"), client.UpdatePage(), confluenceConnection, validUpdatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.UpdatePageBranchUpdated, result.Branch)
	require.Equal(t, confluence.UpdatePageOutput{
		PageID: testPolicyPageID, Title: testPolicyTitle, VersionNumber: 4, WebURL: testSiteBase + "/spaces/OPS/pages/" + testPolicyPageID,
	}, result.Value)
	request := provider.request(0)
	require.Equal(t, http.MethodPut, request.method)
	require.Equal(t, testContentPath+"/pages/"+testPolicyPageID, request.path)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(request.body), &body))
	version := body["version"].(map[string]any)
	require.Regexp(t, `^Annual review \[dex:[0-9a-f]{16}\]$`, version["message"])
	require.Equal(t, float64(4), version["number"])
	require.Equal(t, map[string]any{"representation": "storage", "value": "<p>Staff may work remotely <strong>three</strong> days a week.</p>"}, body["body"])
	require.Equal(t, "current", body["status"])
}

func TestRepeatedUpdateOfOneStepExecutionConvergesOnItsVersion(t *testing.T) {
	page := newVersionedPage(t, 3)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) { page.serve(t, response, request) })
	client := newConfluenceClient(t, provider.URL)
	dexContext := newTestDexContext("update-duplicate")

	first, err := sdkgo.RunMutation(dexContext, client.UpdatePage(), confluenceConnection, validUpdatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.UpdatePageBranchUpdated, first.Branch)
	second, err := sdkgo.RunMutation(dexContext, client.UpdatePage(), confluenceConnection, validUpdatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.UpdatePageBranchUpdated, second.Branch, "the refused duplicate finds its own version")
	require.True(t, second.Value.IsConfirmedByReadBack)
	require.Equal(t, 4, second.Value.VersionNumber)
	require.Equal(t, 4, page.currentVersion(), "the page changed once")

	page.editByAnotherUser()
	third, err := sdkgo.RunMutation(dexContext, client.UpdatePage(), confluenceConnection, validUpdatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.UpdatePageBranchUpdated, third.Branch, "version 4 still carries this Step's marker after a later edit")
	require.Equal(t, 4, third.Value.VersionNumber)
}

func TestUpdateRefusedBecauseAnotherEditWonIsAVersionConflict(t *testing.T) {
	page := newVersionedPage(t, 3)
	page.editByAnotherUser()
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) { page.serve(t, response, request) })
	client := newConfluenceClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newTestDexContext("update-conflict"), client.UpdatePage(), confluenceConnection, validUpdatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.UpdatePageBranchVersionConflict, result.Branch)
	require.Equal(t, 4, result.Value.VersionNumber, "the Value names the current version")
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, 4, page.currentVersion())
	requireNoSentinel(t, result)
}

func TestUpdateRefusedWithTheBaseVersionCurrentIsAProviderRejection(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPut {
			writeJSON(t, response, http.StatusBadRequest, `{"errors":[{"status":400,"title":"SENTINEL invalid storage"}]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(t, testPolicyPageID, testPolicyTitle, 3, "", time.Now(), "<p>x</p>"))
	})
	client := newConfluenceClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newTestDexContext("update-rejected"), client.UpdatePage(), confluenceConnection, validUpdatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.UpdatePageBranchProviderRejected, result.Branch)
	require.Equal(t, "Confluence rejected the page update with HTTP 400", result.Failure.Message)
	requireNoSentinel(t, result)
}

func TestUnconfirmedUpdateIsConfirmedByReadBackOrSentAgain(t *testing.T) {
	page := newVersionedPage(t, 3)
	isFirstPut := true
	var mutex sync.Mutex
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		mutex.Lock()
		isFirst := isFirstPut && request.Method == http.MethodPut
		if isFirst {
			isFirstPut = false
		}
		mutex.Unlock()
		if isFirst {
			page.put(request)
			writeJSON(t, response, http.StatusBadGateway, `{"message":"SENTINEL bad gateway"}`)
			return
		}
		page.serve(t, response, request)
	})
	client := newConfluenceClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newTestDexContext("update-unconfirmed"), client.UpdatePage(), confluenceConnection, validUpdatePageInput())
	require.NoError(t, err)
	require.Equal(t, confluence.UpdatePageBranchUpdated, result.Branch, "the read-back found the applied version")
	require.True(t, result.Value.IsConfirmedByReadBack)
	require.Equal(t, 1, page.putCount())

	unapplied := newVersionedPage(t, 3)
	outage := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPut {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"message":"SENTINEL unavailable"}`)
			return
		}
		unapplied.serve(t, response, request)
	})
	_, err = sdkgo.RunMutation(newTestDexContext("update-outage"), newConfluenceClient(t, outage.URL).UpdatePage(), confluenceConnection, validUpdatePageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 3, unapplied.currentVersion(), "an unchanged page is sent again by the next attempt")
}

func TestUpdatePageMarkerIsStablePerStepExecution(t *testing.T) {
	var messages []string
	var mutex sync.Mutex
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		var body struct {
			Version struct {
				Message string `json:"message"`
			} `json:"version"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		mutex.Lock()
		messages = append(messages, body.Version.Message)
		mutex.Unlock()
		writeJSON(t, response, http.StatusOK, pageJSON(t, testPolicyPageID, testPolicyTitle, 4, body.Version.Message, time.Now(), "<p>x</p>"))
	})
	client := newConfluenceClient(t, provider.URL)
	input := validUpdatePageInput()
	input.VersionMessage = ""
	for _, step := range []string{"step-a", "step-a", "step-b"} {
		_, err := sdkgo.RunMutation(newTestDexContext(step), client.UpdatePage(), confluenceConnection, input)
		require.NoError(t, err)
	}
	require.Equal(t, messages[0], messages[1])
	require.NotEqual(t, messages[0], messages[2])
	require.Regexp(t, `^\[dex:[0-9a-f]{16}\]$`, messages[0])
}

func TestUpdatePageValidatesInputWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingConfluence(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newConfluenceClient(t, provider.URL)
	for _, test := range []struct {
		change  func(*confluence.UpdatePageInput)
		message string
	}{
		{func(input *confluence.UpdatePageInput) { input.NextVersionNumber = 1 }, "nextVersionNumber must be the page's current version number plus one, at least 2"},
		{func(input *confluence.UpdatePageInput) { input.PageID = "OPS" }, "pageId must be a numeric Confluence ID such as 123456"},
		{func(input *confluence.UpdatePageInput) { input.Title = "" }, "title is required"},
		{func(input *confluence.UpdatePageInput) { input.VersionMessage = strings.Repeat("a", 201) }, "versionMessage cannot exceed 200 characters"},
		{func(input *confluence.UpdatePageInput) { input.VersionMessage = "a\nb" }, "versionMessage must be one line without control characters"},
	} {
		input := validUpdatePageInput()
		test.change(&input)
		result, err := sdkgo.RunMutation(newTestDexContext("update-invalid"), client.UpdatePage(), confluenceConnection, input)
		require.NoError(t, err)
		require.Equal(t, confluence.UpdatePageBranchDefect, result.Branch)
		require.Equal(t, test.message, result.Failure.Message)
	}
}
