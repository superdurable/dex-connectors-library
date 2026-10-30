// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const searchPageResponse = `{"results":[
	{"id":"557057","type":"page","status":"current","title":"Remote work policy",
	 "space":{"id":98306,"key":"OPS","name":"Operations"},"version":{"number":3,"when":"2026-09-29T10:00:00.000Z"},
	 "_links":{"webui":"/spaces/OPS/pages/557057/Remote+work+policy"}},
	{"id":"557058","type":"blogpost","status":"current","title":"A blog post"},
	{"id":"557059","type":"page","status":"current","title":"Old remote work note",
	 "space":{"id":98306,"key":"OPS"},"version":{"number":1,"when":"2026-09-26T10:00:00.000Z"},"_links":{"webui":"/spaces/OPS/pages/557059"}}],
	"start":0,"limit":2,"size":3,
	"_links":{"base":"https://ops.atlassian.net/wiki","next":"/rest/api/content/search?cql=type%3Dpage&limit=2&cursor=raNDoM%2BsTRiNg"}}`

func TestSearchPagesSendsEscapedCQLAndReturnsTheNextCursor(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, searchPageResponse)
	})
	client := newConfluenceClient(t, provider.URL)
	modifiedSince := time.Date(2026, time.September, 28, 0, 0, 0, 0, time.UTC)

	result, err := sdkgo.RunQuery(newTestDexContext("search"), client.SearchPages(), confluenceConnection, confluence.SearchPagesInput{
		Filter:   confluence.PageSearchFilter{SpaceKeys: []string{"OPS"}, TitlePhrase: "remote work", Labels: []string{"policy"}, ModifiedSince: &modifiedSince},
		PageSize: 2, Cursor: "previous+cursor",
	})
	require.NoError(t, err)
	require.Equal(t, confluence.SearchPagesBranchSearched, result.Branch)
	wantCQL := `type = page AND space in ("OPS") AND label in ("policy") AND title ~ "\"remote work\"" AND lastmodified >= "2026-09-27" ORDER BY lastmodified DESC`
	require.Equal(t, confluence.SearchPagesOutput{
		CQL: wantCQL,
		Pages: []confluence.PageSummary{{
			ID: "557057", Title: "Remote work policy", Status: "current", SpaceID: "98306", SpaceKey: "OPS", VersionNumber: 3,
			LastModifiedAt: time.Date(2026, time.September, 29, 10, 0, 0, 0, time.UTC),
			WebURL:         "https://ops.atlassian.net/wiki/spaces/OPS/pages/557057/Remote+work+policy",
		}},
		NextCursor: "raNDoM+sTRiNg",
	}, result.Value, "blog posts and pages modified before modifiedSince are removed")
	request := provider.request(0)
	require.Equal(t, testSearchPath+"/content/search", request.path)
	query, err := url.ParseQuery(request.rawQuery)
	require.NoError(t, err)
	require.Equal(t, url.Values{"cql": {wantCQL}, "limit": {"2"}, "cursor": {"previous+cursor"}, "expand": {"space,version"}}, query)
}

func TestSearchPagesRejectsInvalidInputWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingConfluence(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newConfluenceClient(t, provider.URL)
	for _, input := range []confluence.SearchPagesInput{
		{PageSize: 101},
		{Cursor: "has space"},
		{Filter: confluence.PageSearchFilter{AdditionalCQL: "a) OR (b"}},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("search-invalid"), client.SearchPages(), confluenceConnection, input)
		require.NoError(t, err)
		require.Equal(t, confluence.SearchPagesBranchDefect, result.Branch)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	}
}

func TestSearchPagesClassifiesRejectionsAndInvalidPages(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"unparsable CQL", http.StatusBadRequest, `{"statusCode":400,"message":"SENTINEL Could not parse cql"}`, confluence.SearchPagesBranchProviderRejected},
		{"invalid JSON", http.StatusOK, `{"results":`, confluence.SearchPagesBranchInvalidResponse},
		{"page without an ID", http.StatusOK, `{"results":[{"type":"page","title":"x"}],"_links":{}}`, confluence.SearchPagesBranchInvalidResponse},
		{"cursorless next link", http.StatusOK, `{"results":[],"_links":{"next":"/rest/api/content/search?cql=x"}}`, confluence.SearchPagesBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newConfluenceClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newTestDexContext("search-"+test.name), client.SearchPages(), confluenceConnection, confluence.SearchPagesInput{})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Empty(t, result.Value.Pages)
			requireNoSentinel(t, result)
		})
	}
}

func TestSearchPagesRetriesAnOutage(t *testing.T) {
	provider := newRecordingConfluence(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadGateway, `{"message":"SENTINEL bad gateway"}`)
	})
	client := newConfluenceClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newTestDexContext("search-outage"), client.SearchPages(), confluenceConnection, confluence.SearchPagesInput{})
	requireRetry(t, err, sdkgo.FailureAvailability)
}
