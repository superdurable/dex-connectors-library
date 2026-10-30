//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workrequest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go/mondaymock"
)

func TestWorkRequestUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := mondaymock.New(t, ConnectionName)
	harness := newWorkHarnessForConnection(t, mock.Connection())
	outcome := harness.runWorkRequest(t, "mock-default", integrationInput())
	require.Equal(t, WorkItemCreated, outcome.Action)
	require.Equal(t, "5550004", outcome.ItemID)
	require.Equal(t, "february", outcome.GroupID)
	require.Equal(t, "3300001", outcome.UpdateID)

	listCalls := mock.ListItems().Calls()
	require.Len(t, listCalls, 3)
	require.Equal(t, monday.ListItemsInput{
		BoardID: "1234567890", Limit: 25, ColumnIDs: []string{"status", "date4"},
		Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "name", Operator: monday.ItemFilterAnyOf, Values: []string{"Monthly Fire Drill Checklist - February"}}}},
	}, listCalls[0].Input)
	require.Equal(t, "completed-page-2", listCalls[1].Input.Cursor)
	require.Equal(t, "completed-page-3", listCalls[2].Input.Cursor)
	require.Empty(t, mock.GetItem().Calls())
	require.Empty(t, mock.UpdateItemColumnValues().Calls())
	createCalls := mock.CreateItem().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, monday.CreateItemInput{
		BoardID: "1234567890", GroupID: "february", ItemName: "Monthly Fire Drill Checklist - February",
		ColumnValues: map[string]monday.ColumnValue{"status": monday.StatusLabelValue("Working on it"), "date4": monday.DateValue("2026-02-18")},
	}, createCalls[0].Input)
	updateCalls := mock.AddUpdate().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, monday.AddUpdateInput{
		ItemID: "5550004", Body: "Dex created this item: status Working on it, due 2026-02-18.\n\nScheduled from the facilities request.",
	}, updateCalls[0].Input)
}
