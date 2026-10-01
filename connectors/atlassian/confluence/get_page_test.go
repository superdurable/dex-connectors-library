// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetPageConvertsTheStorageBodyAndReturnsTheVersion(t *testing.T) {
	createdAt := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(t, testPolicyPageID, testPolicyTitle, 3, "Annual review", createdAt,
			"<h1>Remote work</h1><p>Staff may work <strong>remotely</strong>.</p><ul><li>Ask first</li></ul>"))
	})
	client := newConfluenceClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newTestDexContext("get-markdown"), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
	require.NoError(t, err)
	require.Equal(t, confluence.GetPageBranchFound, result.Branch)
	require.Equal(t, confluence.Page{
		ID: testPolicyPageID, Title: testPolicyTitle, Status: "current", SpaceID: testSpaceID, ParentPageID: "65537",
		AuthorAccountID: "5b10ac8d82e05b22cc7d4ef5", CreatedAt: createdAt,
		Version: confluence.PageVersion{Number: 3, CreatedAt: createdAt, AuthorAccountID: "5b10ac8d82e05b22cc7d4ef5"},
		Body:    "# Remote work\n\nStaff may work **remotely**.\n\n- Ask first", BodyFormat: confluence.TextFormatMarkdown,
		WebURL: testSiteBase + "/spaces/OPS/pages/" + testPolicyPageID,
	}, result.Value)
}

func TestGetPageReadsAtlasDocFormatAsPlainTextWithinTheLimit(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		document := `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Two days a week.","marks":[{"type":"strong"}]}]}]}`
		page := map[string]any{
			"id": testPolicyPageID, "status": "current", "title": testPolicyTitle, "spaceId": testSpaceID,
			"version": map[string]any{"number": 2}, "body": map[string]any{"atlas_doc_format": map[string]any{"representation": "atlas_doc_format", "value": document}},
		}
		encoded, err := json.Marshal(page)
		require.NoError(t, err)
		writeJSON(t, response, http.StatusOK, string(encoded))
	})
	client := newConfluenceClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newTestDexContext("get-adf"), client.GetPage(), confluenceConnection, confluence.GetPageInput{
		PageID: testPolicyPageID, BodyRepresentation: confluence.BodyRepresentationAtlasDocFormat, BodyFormat: confluence.TextFormatPlainText, MaxBodyCharacters: 8,
	})
	require.NoError(t, err)
	require.Equal(t, confluence.GetPageBranchFound, result.Branch)
	require.Equal(t, "Two days", result.Value.Body)
	require.True(t, result.Value.IsBodyTruncated)
	require.Equal(t, 2, result.Value.Version.Number)
	require.Equal(t, "body-format=atlas_doc_format", provider.request(0).rawQuery)
}

func TestGetPageSelectsNotFoundAndInvalidResponse(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"missing page", http.StatusNotFound, `{"errors":[{"status":404,"title":"SENTINEL Not Found"}]}`, confluence.GetPageBranchNotFound},
		{"forbidden", http.StatusForbidden, `{"errors":[{"status":403,"title":"SENTINEL Forbidden"}]}`, confluence.GetPageBranchProviderRejected},
		{"no body", http.StatusOK, `{"id":"557057","title":"x","spaceId":"98306","version":{"number":1}}`, confluence.GetPageBranchInvalidResponse},
		{"no version", http.StatusOK, `{"id":"557057","title":"x","spaceId":"98306","body":{"storage":{"value":"<p>x</p>"}}}`, confluence.GetPageBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newConfluenceClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newTestDexContext("get-"+test.name), client.GetPage(), confluenceConnection, confluence.GetPageInput{PageID: testPolicyPageID})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			requireNoSentinel(t, result)
		})
	}
}

func TestGetPageValidatesInputWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingConfluence(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newConfluenceClient(t, provider.URL)
	for _, input := range []confluence.GetPageInput{
		{PageID: "../spaces"},
		{PageID: testPolicyPageID, BodyRepresentation: "view"},
		{PageID: testPolicyPageID, BodyFormat: "html"},
		{PageID: testPolicyPageID, MaxBodyCharacters: 131073},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("get-invalid"), client.GetPage(), confluenceConnection, input)
		require.NoError(t, err)
		require.Equal(t, confluence.GetPageBranchDefect, result.Branch)
	}
}
