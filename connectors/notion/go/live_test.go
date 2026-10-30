//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestLiveSearchQueryCreateUpdateAndRead exercises every operation against a real Notion workspace.
// NOTION_CONNECTOR_TEST_TOKEN is an internal connection API token, and
// NOTION_CONNECTOR_TEST_DATA_SOURCE_ID names a disposable data source shared with it whose title
// property is the only property the test writes. The test creates one row and leaves it for
// manual cleanup, because trashing a page is outside this connector's operations.
func TestLiveSearchQueryCreateUpdateAndRead(t *testing.T) {
	token, dataSourceID := os.Getenv("NOTION_CONNECTOR_TEST_TOKEN"), os.Getenv("NOTION_CONNECTOR_TEST_DATA_SOURCE_ID")
	if token == "" || dataSourceID == "" {
		t.Skip("NOTION_CONNECTOR_TEST_TOKEN and NOTION_CONNECTOR_TEST_DATA_SOURCE_ID are not configured")
	}
	client, err := notion.New(notion.Config{}, sdkgo.StaticCredentialProvider[notion.Credentials]{
		notionConnection: {APIToken: sdkgo.NewSecretString(token)},
	})
	require.NoError(t, err)

	search, err := sdkgo.RunQuery(newNotionDexContext("live-search"), client.Search(), notionConnection,
		notion.SearchInput{ObjectType: notion.SearchObjectDataSource, PageSize: 5})
	require.NoError(t, err)
	require.Contains(t, []sdkgo.BranchID{notion.SearchBranchSearched, notion.SearchBranchNoMatch}, search.Branch)

	title := "Dex live test " + time.Now().UTC().Format(time.RFC3339Nano)
	created, err := sdkgo.RunMutation(newNotionDexContext("live-create"), client.CreatePage(), notionConnection, notion.CreatePageInput{
		DataSourceID: dataSourceID, Properties: map[string]notion.PropertyValue{notion.TitlePropertyID: notion.TitleValue(title)},
		BodyText: "Created by the Notion connector live test.",
	})
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, created.Branch, "%+v", created.Failure)

	updated, err := sdkgo.RunMutation(newNotionDexContext("live-update"), client.UpdatePageProperties(), notionConnection, notion.UpdatePagePropertiesInput{
		PageID: created.Value.PageID, Properties: map[string]notion.PropertyValue{notion.TitlePropertyID: notion.TitleValue(title + " (updated)")},
	})
	require.NoError(t, err)
	require.Equal(t, notion.UpdatePagePropertiesBranchUpdated, updated.Branch, "%+v", updated.Failure)

	read, err := sdkgo.RunQuery(newNotionDexContext("live-get"), client.GetPage(), notionConnection, notion.GetPageInput{PageID: created.Value.PageID})
	require.NoError(t, err)
	require.Equal(t, notion.GetPageBranchFound, read.Branch)
	require.Equal(t, title+" (updated)", read.Value.Page.Title)
	require.Equal(t, "Created by the Notion connector live test.", read.Value.Content.Text)

	query, err := sdkgo.RunQuery(newNotionDexContext("live-query"), client.QueryDatabase(), notionConnection, notion.QueryDatabaseInput{
		DataSourceID: dataSourceID,
		Filter:       &notion.QueryFilter{Property: notion.TitlePropertyID, Type: notion.PropertyTypeTitle, Condition: notion.FilterEquals, Text: title + " (updated)"},
		PageSize:     5,
	})
	require.NoError(t, err)
	require.Equal(t, notion.QueryDatabaseBranchQueried, query.Branch)
	require.Len(t, query.Value.Pages, 1)
}
