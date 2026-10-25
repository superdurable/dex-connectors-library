//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package submissionintake

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/connectors/notion/notionmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/connectormock"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	mockDatabaseID   = "2a4d6f80-1b3c-4d5e-8f70-112233445500"
	mockRowID        = "2a4d6f80-1b3c-4d5e-8f70-112233445577"
	mockTrashedRowID = "2a4d6f80-1b3c-4d5e-8f70-112233445588"
	mockFirstRowID   = "2a4d6f80-1b3c-4d5e-8f70-1122334455a1"
	mockSecondRowID  = "2a4d6f80-1b3c-4d5e-8f70-1122334455a2"
	mockThirdRowID   = "2a4d6f80-1b3c-4d5e-8f70-1122334455a3"
	mockSecondSource = "2a4d6f80-1b3c-4d5e-8f70-112233445599"
	mockSubmissionID = "sub-new-1"
)

// The mock defaults fit the database title "Form submissions" and the submission ID "sub-new-1";
// any other input needs a script, as the tests below show.
func TestSubmissionIntakeUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := notionmock.New(t, ConnectionName)
	flow, harness := newNotionIntegrationHarnessWithConnection(t, mock.Connection())
	record := runSubmissionFlow(t, integrationContext(t), harness, flow, "mock-default", submissionInput("sub-new-1"))
	require.Equal(t, PhaseUpdated, record.Phase)
	require.True(t, record.IsExistingRow)
	require.Equal(t, submissionsSourceID, record.DataSourceID, "the decoy, the trashed twin, and the repeated source resolve to one data source")
	require.Equal(t, mockRowID, record.PageID, "the trashed and archived rows are ignored and the repeated row counts once")
	require.Empty(t, record.DuplicatePageIDs)

	require.Equal(t, []string{"", "search-page-2", "search-page-3"}, searchCursors(mock), "the Flow follows all three search pages")
	require.Equal(t, []string{"", "rows-page-2", "rows-page-3"}, queryCursors(mock), "the Flow reads past the trashed first page to the saved row and its repeat")
	require.Empty(t, mock.CreatePage().Calls())
	updates := mock.UpdatePageProperties().Calls()
	require.Len(t, updates, 1)
	require.Equal(t, notion.UpdatePagePropertiesInput{
		PageID: mockRowID,
		Properties: map[string]notion.PropertyValue{
			notion.TitlePropertyID: notion.TitleValue("Ada Lovelace"),
			EmailProperty:          notion.EmailValue("ada@example.com"),
			SubmittedAtProperty:    notion.DateValue(record.SubmittedAt),
		},
	}, updates[0].Input)
	reads := mock.GetPage().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, mockRowID, reads[0].Input.PageID)
}

func TestSubmissionIntakeFindsDuplicateRowsOnDifferentMockPagesWithRealDex(t *testing.T) {
	mock := notionmock.New(t, ConnectionName)
	mock.QueryDatabase().Respond(
		mockRowsCase("rows-a", mockRow(mockFirstRowID)),
		mockRowsCase("rows-b", mockRow(mockSecondRowID)),
		mockRowsCase("", mockRow(mockThirdRowID)),
	)
	flow, harness := newNotionIntegrationHarnessWithConnection(t, mock.Connection())
	record := runSubmissionFlow(t, integrationContext(t), harness, flow, "mock-duplicates", submissionInput(mockSubmissionID))

	require.Equal(t, PhaseDuplicateRows, record.Phase)
	require.Equal(t, []string{mockFirstRowID, mockSecondRowID}, record.DuplicatePageIDs, "the scan stops once two rows make the duplicate certain")
	require.Equal(t, []string{"", "rows-a"}, queryCursors(mock), "the third page is never read")
	require.Empty(t, mock.CreatePage().Calls())
	require.Empty(t, mock.UpdatePageProperties().Calls())
	require.Empty(t, mock.GetPage().Calls())
}

func TestSubmissionIntakeFindsAmbiguousDataSourcesOnDifferentMockPagesWithRealDex(t *testing.T) {
	mock := notionmock.New(t, ConnectionName)
	mock.Search().Respond(
		notionmock.SearchSearched(mockSearchPage("search-a", mockDataSource(submissionsSourceID)), nil),
		notionmock.SearchSearched(mockSearchPage("", mockDataSource(mockSecondSource)), nil),
	)
	flow, harness := newNotionIntegrationHarnessWithConnection(t, mock.Connection())
	record := runSubmissionFlow(t, integrationContext(t), harness, flow, "mock-ambiguous", submissionInput(mockSubmissionID))

	require.Equal(t, PhaseDataSourceAmbiguous, record.Phase)
	require.ElementsMatch(t, []string{submissionsSourceID, mockSecondSource}, record.CandidateDataSourceIDs)
	require.Equal(t, []string{"", "search-a"}, searchCursors(mock))
	require.Empty(t, mock.QueryDatabase().Calls())
	require.Empty(t, mock.CreatePage().Calls())
	require.Empty(t, mock.UpdatePageProperties().Calls())
}

func TestSubmissionIntakeScansFailOnRepeatedCursorsAndTheirPageLimitWithRealDex(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		script       func(*notionmock.Mock)
		wantMessage  string
		wantSearches int
		wantQueries  int
	}{
		{
			name: "query-repeated-cursor",
			script: func(mock *notionmock.Mock) {
				mock.QueryDatabase().Respond(mockRowsCase("cursor-a"), mockRowsCase("cursor-b"), mockRowsCase("cursor-a"))
			},
			wantMessage: queryRepeatedCursorFailure, wantSearches: 3, wantQueries: 3,
		},
		{
			name: "query-page-limit",
			script: func(mock *notionmock.Mock) {
				pages := make([]notion.QueryDatabaseOutput, maximumScanPages)
				for index := range pages {
					pages[index] = mockRowsPage(fmt.Sprintf("cursor-%d", index+1))
				}
				pageNumber := connectormock.CursorPageNumber[notion.QueryDatabaseInput]("startCursor", "nextCursor", pages)
				mock.QueryDatabase().When(func(notion.QueryDatabaseInput) bool { return true },
					connectormock.Paged(notion.QueryDatabaseBranchQueried, pageNumber, pages...))
			},
			wantMessage: queryPageLimitFailure, wantSearches: 3, wantQueries: maximumScanPages,
		},
		{
			name: "search-repeated-cursor",
			script: func(mock *notionmock.Mock) {
				mock.Search().Respond(
					notionmock.SearchSearched(mockSearchPage("cursor-a"), nil),
					notionmock.SearchSearched(mockSearchPage("cursor-b"), nil),
					notionmock.SearchSearched(mockSearchPage("cursor-a"), nil),
				)
			},
			wantMessage: searchRepeatedCursorFailure, wantSearches: 3,
		},
		{
			name: "search-page-limit",
			script: func(mock *notionmock.Mock) {
				pages := make([]notion.SearchOutput, maximumScanPages)
				for index := range pages {
					pages[index] = mockSearchPage(fmt.Sprintf("cursor-%d", index+1))
				}
				pageNumber := connectormock.CursorPageNumber[notion.SearchInput]("startCursor", "nextCursor", pages)
				mock.Search().When(func(notion.SearchInput) bool { return true },
					connectormock.Paged(notion.SearchBranchSearched, pageNumber, pages...))
			},
			wantMessage: searchPageLimitFailure, wantSearches: maximumScanPages,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			mock := notionmock.New(t, ConnectionName)
			scenario.script(mock)
			flow, harness := newNotionIntegrationHarnessWithConnection(t, mock.Connection())
			ctx := integrationContext(t)
			flowID := startSubmissionFlow(t, ctx, harness, flow, "mock-"+scenario.name, submissionInput(mockSubmissionID))
			result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
			require.NoError(t, err)
			require.Equal(t, dex.FlowFailed, result.Status)
			require.Contains(t, result.ErrorMessage, scenario.wantMessage)
			require.Len(t, mock.Search().Calls(), scenario.wantSearches)
			require.Len(t, mock.QueryDatabase().Calls(), scenario.wantQueries, "an unfinished data source search must not query rows")
			require.Empty(t, mock.CreatePage().Calls(), "an unfinished scan must not create a row")
			require.Empty(t, mock.UpdatePageProperties().Calls(), "an unfinished scan must not update a row")
		})
	}
}

func TestSubmissionIntakeReconcilesAnUncertainCreateAcrossMockPagesWithRealDex(t *testing.T) {
	mock := notionmock.New(t, ConnectionName)
	mock.QueryDatabase().Respond(
		// The first scan reads a trashed row and an empty page, so the Flow creates the row.
		mockRowsCase("find-2", mockTrashedRow()),
		mockRowsCase(""),
		// The reconciliation scan starts over and finds the row the uncertain create saved on its second page.
		mockRowsCase("reconcile-2"),
		mockRowsCase("", mockRow(mockRowID)),
	)
	mock.CreatePage().Respond(notionmock.CreatePageUncertain(notion.CreatePageOutput{},
		sdkgo.Failure{Kind: sdkgo.FailureTransport, Message: "The request timed out after it was sent."}))
	flow, harness := newNotionIntegrationHarnessWithConnection(t, mock.Connection())
	record := runSubmissionFlow(t, integrationContext(t), harness, flow, "mock-reconcile", submissionInput(mockSubmissionID))

	require.Equal(t, PhaseCreated, record.Phase, "the reconciliation scan adopted the saved row")
	require.False(t, record.IsExistingRow)
	require.NotNil(t, record.UncertainCreate)
	require.Equal(t, mockRowID, record.PageID)
	require.Equal(t, []string{"", "find-2", "", "reconcile-2"}, queryCursors(mock), "each scan starts at the first page and follows its own cursors")
	require.Len(t, mock.CreatePage().Calls(), 1, "an uncertain create is never sent again")
	require.Empty(t, mock.UpdatePageProperties().Calls())
	reads := mock.GetPage().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, mockRowID, reads[0].Input.PageID)
}

func searchCursors(mock *notionmock.Mock) []string {
	var cursors []string
	for _, call := range mock.Search().Calls() {
		cursors = append(cursors, call.Input.StartCursor)
	}
	return cursors
}

func queryCursors(mock *notionmock.Mock) []string {
	var cursors []string
	for _, call := range mock.QueryDatabase().Calls() {
		cursors = append(cursors, call.Input.StartCursor)
	}
	return cursors
}

func mockRow(id string) notion.Page {
	return notion.Page{
		ID: id, Title: "Ada Lovelace",
		Parent: notion.Parent{Type: notion.ParentTypeDataSource, ID: submissionsSourceID, DatabaseID: mockDatabaseID},
		Properties: map[string]notion.PageProperty{
			SubmissionIDProperty: {ID: "submission-id", PropertyValue: notion.RichTextValue(mockSubmissionID), PlainText: mockSubmissionID},
		},
	}
}

func mockTrashedRow() notion.Page {
	row := mockRow(mockTrashedRowID)
	row.IsInTrash = true
	return row
}

func mockRowsPage(nextCursor string, rows ...notion.Page) notion.QueryDatabaseOutput {
	return notion.QueryDatabaseOutput{DataSourceID: submissionsSourceID, Pages: rows, NextCursor: nextCursor, HasMore: nextCursor != ""}
}

func mockRowsCase(nextCursor string, rows ...notion.Page) connectormock.Case[notion.QueryDatabaseOutput] {
	return notionmock.QueryDatabaseQueried(mockRowsPage(nextCursor, rows...), nil)
}

func mockDataSource(id string) notion.SearchMatch {
	return notion.SearchMatch{
		ObjectType: notion.SearchObjectDataSource, ID: id, Title: submissionsTitle,
		Parent: notion.Parent{Type: notion.ParentTypeDatabase, ID: mockDatabaseID},
	}
}

func mockSearchPage(nextCursor string, matches ...notion.SearchMatch) notion.SearchOutput {
	return notion.SearchOutput{Matches: matches, NextCursor: nextCursor, HasMore: nextCursor != ""}
}
