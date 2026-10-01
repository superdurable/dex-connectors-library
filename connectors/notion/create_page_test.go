// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validCreatePageInput() notion.CreatePageInput {
	return notion.CreatePageInput{
		DataSourceID: testDataSourceID,
		Properties: map[string]notion.PropertyValue{
			"Name": notion.TitleValue("Ada Lovelace"), "Email": notion.EmailValue("ada@example.com"),
			"Status": notion.SelectValue("New"),
		},
		BodyText: "Hello from the form.\nSecond line.\n\n\nNext paragraph.",
	}
}

func TestCreatePageSendsADataSourceParentPropertiesAndParagraphs(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newNotionDexContext("create"), client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, testPageID, result.Value.PageID)
	require.Equal(t, testDataSourceID, result.Value.DataSourceID)
	require.NotNil(t, result.Value.Page)
	require.Equal(t, "Ada Lovelace", result.Value.Page.Title)
	require.False(t, result.Value.IsConfirmedAfterTimeout)
	require.Equal(t, testPageID, result.Receipt.ProviderObjectID)
	require.Equal(t, sdkgo.IdempotencyKey(result.Receipt.CallID), result.Receipt.IdempotencyKey)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/v1/pages", request.path)
	require.JSONEq(t, `{
		"parent":{"type":"data_source_id","data_source_id":"`+testDataSourceID+`"},
		"properties":{
			"Name":{"title":[{"type":"text","text":{"content":"Ada Lovelace"}}]},
			"Email":{"email":"ada@example.com"},
			"Status":{"select":{"name":"New"}}
		},
		"children":[
			{"object":"block","type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"Hello from the form.\nSecond line."}}]}},
			{"object":"block","type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"Next paragraph."}}]}}
		]}`, request.body)
}

func TestCreatePageResolvesADatabaseIDBeforeWriting(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, databaseJSON(testDataSourceID))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	input := validCreatePageInput()
	input.DataSourceID, input.DatabaseID, input.BodyText = "", testDatabaseID, ""
	result, err := sdkgo.RunMutation(newNotionDexContext("create-database"), client.CreatePage(), notionConnection, input)
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, "/v1/databases/"+testDatabaseID, provider.request(0).path)
	require.NotContains(t, provider.request(1).body, "children")
	require.Contains(t, provider.request(1).body, `"data_source_id":"`+testDataSourceID+`"`)
}

func TestCreatePageWritesNothingWhenTheDatabaseLookupFails(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{name: "unshared database", status: http.StatusNotFound, body: notionError(404, "object_not_found"), branch: notion.CreatePageBranchNotFound},
		{name: "several data sources", status: http.StatusOK, body: databaseJSON(testDataSourceID, "5d7f9213-4e6f-4081-92a3-445566778899"), branch: notion.CreatePageBranchDefect},
		{name: "invalid database", status: http.StatusOK, body: `{"object":"page"}`, branch: notion.CreatePageBranchInvalidResponse},
		{name: "unavailable lookup", status: http.StatusServiceUnavailable, body: notionError(503, "service_unavailable"), isRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newNotionClient(t, provider.URL)
			input := validCreatePageInput()
			input.DataSourceID, input.DatabaseID = "", testDatabaseID
			result, err := sdkgo.RunMutation(newNotionDexContext("lookup"), client.CreatePage(), notionConnection, input)
			if test.isRetry {
				requireRetry(t, err, sdkgo.FailureAvailability)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.branch, result.Branch)
				requireNoSentinel(t, result)
			}
			require.Equal(t, 1, provider.requestCount(), "the create is never sent")
		})
	}
}

// TestCreatePageNeverResendsARequestNotionMayHaveReceived records every status Notion documents for a create.
func TestCreatePageNeverResendsARequestNotionMayHaveReceived(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		isRetry bool
	}{
		{name: "validation error", status: http.StatusBadRequest, body: notionError(400, "validation_error"), branch: notion.CreatePageBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "missing capability or block limit", status: http.StatusForbidden, body: notionError(403, "restricted_resource"), branch: notion.CreatePageBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "unshared data source", status: http.StatusNotFound, body: notionError(404, "object_not_found"), branch: notion.CreatePageBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "row limit", status: http.StatusNotAcceptable, body: notionError(406, "row_limit_exceeded"), branch: notion.CreatePageBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "conflict", status: http.StatusConflict, body: notionError(409, "conflict_error"), branch: notion.CreatePageBranchUncertain, kind: sdkgo.FailureConflict},
		{name: "internal error", status: http.StatusInternalServerError, body: notionError(500, "internal_server_error"), branch: notion.CreatePageBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "bad gateway", status: http.StatusBadGateway, body: notionError(502, "bad_gateway"), branch: notion.CreatePageBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "503 without a saved page", status: http.StatusServiceUnavailable, body: notionError(503, "service_unavailable"), branch: notion.CreatePageBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, body: notionError(504, "gateway_timeout"), branch: notion.CreatePageBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "unusable 200", status: http.StatusOK, body: `{"object":"page"}`, branch: notion.CreatePageBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "rate limited", status: http.StatusTooManyRequests, body: notionError(429, "rate_limited"), isRetry: true, kind: sdkgo.FailureRateLimit},
		{name: "overloaded", status: 529, body: notionError(529, "service_overload"), isRetry: true, kind: sdkgo.FailureRateLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newNotionClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newNotionDexContext("create-"+test.name), client.CreatePage(), notionConnection, validCreatePageInput())
			if test.isRetry {
				requireRetry(t, err, test.kind)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.branch, result.Branch)
				require.Equal(t, test.kind, result.Failure.Kind)
				require.Empty(t, result.Value.PageID)
				require.Equal(t, testDataSourceID, result.Value.DataSourceID)
				requireNoSentinel(t, result)
			}
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestCreatePageConfirmsAPageNotionSavedBeforeA503(t *testing.T) {
	const savedPageID = "6e8a0324-5f70-4192-a3b4-556677889900"
	saved := `{"object":"error","status":503,"code":"service_unavailable","message":"SENTINEL saved",` +
		`"additional_data":{"retry_guidance":["Read the object again to confirm the saved change.","Do not repeat the write."],` +
		`"committed_resource_id":"` + savedPageID + `"}}`
	for _, test := range []struct {
		name         string
		readStatus   int
		isPageLoaded bool
	}{
		{name: "the saved page is read back", readStatus: http.StatusOK, isPageLoaded: true},
		{name: "the read-back also fails", readStatus: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					writeJSON(t, response, http.StatusServiceUnavailable, saved)
					return
				}
				if test.readStatus != http.StatusOK {
					writeJSON(t, response, test.readStatus, notionError(test.readStatus, "service_unavailable"))
					return
				}
				writeJSON(t, response, http.StatusOK, pageJSON(savedPageID, "Ada Lovelace"))
			})
			client := newNotionClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newNotionDexContext("saved"), client.CreatePage(), notionConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
			require.Equal(t, savedPageID, result.Value.PageID)
			require.True(t, result.Value.IsConfirmedAfterTimeout)
			require.Equal(t, test.isPageLoaded, result.Value.Page != nil)
			require.Equal(t, savedPageID, result.Receipt.ProviderObjectID)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodGet, provider.request(1).method)
			require.Equal(t, "/v1/pages/"+savedPageID, provider.request(1).path)
			requireNoSentinel(t, result)
		})
	}
}

func TestCreatePageTimeoutAfterDispatchIsUncertain(t *testing.T) {
	release := make(chan struct{})
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		<-release
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	t.Cleanup(func() { close(release) })
	client := newNotionClient(t, provider.URL, notion.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	result, err := sdkgo.RunMutation(newNotionDexContext("timeout"), client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreatePageRetriesWhenNotionWasNeverReached(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newNotionClient(t, "http://"+address)
	_, err = sdkgo.RunMutation(newNotionDexContext("unreachable"), client.CreatePage(), notionConnection, validCreatePageInput())
	retry := requireRetry(t, err, sdkgo.FailureTransport)
	require.Contains(t, retry.Failure.Message, "no page creation was written")
}

func TestCreatePageRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingNotion(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newNotionClient(t, provider.URL)
	tooManyParagraphs := strings.Repeat("paragraph\n\n", 101)
	for name, input := range map[string]notion.CreatePageInput{
		"no parent":           {Properties: map[string]notion.PropertyValue{"Name": notion.TitleValue("Ada")}},
		"two parents":         {DataSourceID: testDataSourceID, DatabaseID: testDatabaseID},
		"an invalid value":    {DataSourceID: testDataSourceID, Properties: map[string]notion.PropertyValue{"Email": notion.EmailValue("not an email")}},
		"too many paragraphs": {DataSourceID: testDataSourceID, BodyText: tooManyParagraphs},
		"too much body text":  {DataSourceID: testDataSourceID, BodyText: strings.Repeat("a", notion.MaximumBodyTextCharacters+1)},
	} {
		result, err := sdkgo.RunMutation(newNotionDexContext("invalid"), client.CreatePage(), notionConnection, input)
		require.NoError(t, err)
		require.Equal(t, notion.CreatePageBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}
