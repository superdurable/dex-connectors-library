// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func textBlockJSON(blockType string, text string, extra string) string {
	return fmt.Sprintf(`{"object":"block","id":"b-%s","type":%q,"has_children":false,%q:{"rich_text":[{"plain_text":%q}]%s}}`,
		blockType, blockType, blockType, text, extra)
}

func blockListJSON(blocks []string, nextCursor string) string {
	if nextCursor == "" {
		return `{"object":"list","results":[` + strings.Join(blocks, ",") + `],"next_cursor":null,"has_more":false}`
	}
	return `{"object":"list","results":[` + strings.Join(blocks, ",") + `],"next_cursor":"` + nextCursor + `","has_more":true}`
}

func TestGetPageRendersTopLevelBlocksAsPlainText(t *testing.T) {
	blocks := []string{
		textBlockJSON("heading_2", "Submission", ""),
		textBlockJSON("paragraph", "Hello from the form.", ""),
		textBlockJSON("bulleted_list_item", "first", ""),
		textBlockJSON("numbered_list_item", "one", ""),
		textBlockJSON("numbered_list_item", "two", ""),
		textBlockJSON("to_do", "follow up", `,"checked":true`),
		textBlockJSON("to_do", "archive", `,"checked":false`),
		`{"object":"block","id":"b-toggle","type":"toggle","has_children":true,"toggle":{"rich_text":[{"plain_text":"Details"}]}}`,
		`{"object":"block","id":"b-div","type":"divider","has_children":false,"divider":{}}`,
		`{"object":"block","id":"b-img","type":"image","has_children":false,"image":{"caption":[],"type":"external"}}`,
		`{"object":"block","id":"b-row","type":"table_row","has_children":false,"table_row":{"cells":[[{"plain_text":"a"}],[{"plain_text":"b"}]]}}`,
		`{"object":"block","id":"b-child","type":"child_database","has_children":false,"child_database":{"title":"Follow-ups"}}`,
		textBlockJSON("numbered_list_item", "restart", ""),
	}
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/children") {
			writeJSON(t, response, http.StatusOK, blockListJSON(blocks, ""))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("get"), client.GetPage(), notionConnection, notion.GetPageInput{
		PageID: "https://app.notion.com/p/" + strings.ReplaceAll(testPageID, "-", ""),
	})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchFound, result.Branch)
	require.Equal(t, "Ada Lovelace", result.Value.Page.Title)
	require.Equal(t, notion.PageContent{
		Text:       "Submission\nHello from the form.\n- first\n1. one\n2. two\n[x] follow up\n[ ] archive\nDetails\na | b\nFollow-ups\n1. restart",
		BlockCount: 13, NestedBlockCount: 1, SkippedBlockCount: 2,
	}, result.Value.Content)
	require.Equal(t, "/v1/pages/"+testPageID, provider.request(0).path)
	require.Equal(t, "/v1/blocks/"+testPageID+"/children", provider.request(1).path)
	require.Equal(t, "page_size=100", provider.request(1).query)
}

func TestGetPageFollowsBlockCursorsUpToMaxBlocks(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if !strings.HasSuffix(request.URL.Path, "/children") {
			writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Long page"))
			return
		}
		switch request.URL.Query().Get("start_cursor") {
		case "":
			writeJSON(t, response, http.StatusOK, blockListJSON([]string{textBlockJSON("paragraph", "one", ""), textBlockJSON("paragraph", "two", "")}, "cursor-2"))
		case "cursor-2":
			writeJSON(t, response, http.StatusOK, blockListJSON([]string{textBlockJSON("paragraph", "three", "")}, "cursor-3"))
		default:
			t.Errorf("unexpected cursor %q", request.URL.Query().Get("start_cursor"))
		}
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("paged"), client.GetPage(), notionConnection, notion.GetPageInput{PageID: testPageID, MaxBlocks: 3})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchFound, result.Branch)
	require.Equal(t, "one\ntwo\nthree", result.Value.Content.Text)
	require.Equal(t, 3, result.Value.Content.BlockCount)
	require.True(t, result.Value.Content.HasMoreBlocks)
	require.Equal(t, 3, provider.requestCount())
	second, err := url.ParseQuery(provider.request(2).query)
	require.NoError(t, err)
	require.Equal(t, url.Values{"page_size": {"1"}, "start_cursor": {"cursor-2"}}, second)
}

func TestGetPageTruncatesTextAtMaxTextCharacters(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/children") {
			writeJSON(t, response, http.StatusOK, blockListJSON([]string{textBlockJSON("paragraph", "abcdef", ""), textBlockJSON("paragraph", "ghij", "")}, ""))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("truncate"), client.GetPage(), notionConnection, notion.GetPageInput{PageID: testPageID, MaxTextCharacters: 9})
	require.NoError(t, err)
	require.Equal(t, "abcdef\ngh", result.Value.Content.Text)
	require.True(t, result.Value.Content.IsTextTruncated)
	require.Equal(t, 2, result.Value.Content.BlockCount)
}

func TestGetPageSkipsContentWhenAsked(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newNotionDexContext("skip"), client.GetPage(), notionConnection, notion.GetPageInput{PageID: testPageID, ShouldSkipContent: true})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchFound, result.Branch)
	require.Equal(t, notion.PageContent{}, result.Value.Content)
	require.Equal(t, 1, provider.requestCount())
}

func TestGetPageMapsMissingPagesAndFailedBlockReads(t *testing.T) {
	missing := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, notionError(404, "object_not_found"))
	})
	result, err := sdkgo.RunQuery(newNotionDexContext("missing"), newNotionClient(t, missing.URL).GetPage(), notionConnection, notion.GetPageInput{PageID: testPageID})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchNotFound, result.Branch)
	requireNoSentinel(t, result)

	unavailable := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/children") {
			writeJSON(t, response, http.StatusServiceUnavailable, notionError(503, "service_unavailable"))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	_, err = sdkgo.RunQuery(newNotionDexContext("unavailable"), newNotionClient(t, unavailable.URL).GetPage(), notionConnection, notion.GetPageInput{PageID: testPageID})
	requireRetry(t, err, sdkgo.FailureAvailability)
}

func TestGetPageRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingNotion(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newNotionClient(t, provider.URL)
	for _, input := range []notion.GetPageInput{
		{}, {PageID: "Ada"}, {PageID: testPageID, MaxBlocks: 501}, {PageID: testPageID, MaxTextCharacters: -1},
	} {
		result, err := sdkgo.RunQuery(newNotionDexContext("invalid"), client.GetPage(), notionConnection, input)
		require.NoError(t, err)
		require.Equal(t, notion.GetPageBranchDefect, result.Branch)
	}
	require.Zero(t, provider.requestCount())
}
