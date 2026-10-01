// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workrequest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validInput() Input {
	return Input{
		BoardID: " 1234567890 ", GroupID: "topics", ItemName: " Monthly Fire Drill Checklist - February ",
		StatusColumnID: "status", StatusLabel: "Working on it", DueDateColumnID: "date4", DueDate: "2026-02-18",
		UpdateText: "Scheduled from the facilities request.",
	}
}

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordWorkRequestStepType, dex.GetFinalStepType[Input](recordWorkRequest{}))
	require.Equal(t, chooseExistingItemStepType, dex.GetFinalStepType[monday.ListItemsResult](chooseExistingItem{}))
	require.Equal(t, confirmExistingItemStepType, dex.GetFinalStepType[monday.GetItemResult](confirmExistingItem{}))
	require.Equal(t, recordScheduledItemStepType, dex.GetFinalStepType[monday.UpdateItemColumnValuesResult](recordScheduledItem{}))
	require.Equal(t, recordCreatedItemStepType, dex.GetFinalStepType[monday.CreateItemResult](recordCreatedItem{}))
	require.Equal(t, recordUncertainItemStepType, dex.GetFinalStepType[monday.CreateItemResult](recordUncertainItem{}))
	require.Equal(t, completeWorkRequestStepType, dex.GetFinalStepType[monday.AddUpdateResult](completeWorkRequest{}))
	require.Equal(t, recordUncertainUpdateStepType, dex.GetFinalStepType[monday.AddUpdateResult](recordUncertainUpdate{}))
	wait, err := recordWorkRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, ConnectionName))})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"))})
	})
}

func TestBuildWorkRequestValidatesStartInput(t *testing.T) {
	request, err := BuildWorkRequest(validInput())
	require.NoError(t, err)
	require.Equal(t, WorkRequest{
		BoardID: "1234567890", GroupID: "topics", ItemName: "Monthly Fire Drill Checklist - February",
		StatusColumnID: "status", StatusLabel: "Working on it", DueDateColumnID: "date4", DueDate: "2026-02-18",
		UpdateText: "Scheduled from the facilities request.",
	}, request)
	for name, change := range map[string]func(*Input){
		"board name":         func(input *Input) { input.BoardID = "Facilities" },
		"blank item name":    func(input *Input) { input.ItemName = " " },
		"blank status label": func(input *Input) { input.StatusLabel = "" },
		"same column twice":  func(input *Input) { input.DueDateColumnID = "status" },
		"day-first date":     func(input *Input) { input.DueDate = "18/02/2026" },
		"blank update":       func(input *Input) { input.UpdateText = "" },
	} {
		input := validInput()
		change(&input)
		_, err := BuildWorkRequest(input)
		require.ErrorIs(t, err, errInvalidWorkRequest, name)
	}
}

func TestMappersPassOnlyTheRecordedRequest(t *testing.T) {
	request, err := BuildWorkRequest(validInput())
	require.NoError(t, err)
	require.Equal(t, monday.ListItemsInput{
		BoardID: "1234567890", Limit: searchPageLimit, ColumnIDs: []string{"status", "date4"},
		Filter: monday.ItemFilter{Rules: []monday.ItemFilterRule{{ColumnID: "name", Operator: monday.ItemFilterAnyOf, Values: []string{"Monthly Fire Drill Checklist - February"}}}},
	}, MapToListItemsInput(newItemSearch(request, "")))
	require.Equal(t, monday.ListItemsInput{BoardID: "1234567890", Cursor: "next", Limit: searchPageLimit, ColumnIDs: []string{"status", "date4"}},
		MapToListItemsInput(newItemSearch(request, "next")), "a cursor carries its own filter")
	require.Equal(t, monday.GetItemInput{ItemID: "5550001", IncludesInactive: true}, MapToGetItemInput(ItemReference{BoardID: request.BoardID, ItemID: "5550001"}))
	schedule := map[string]monday.ColumnValue{"status": monday.StatusLabelValue("Working on it"), "date4": monday.DateValue("2026-02-18")}
	require.Equal(t, monday.UpdateItemColumnValuesInput{BoardID: "1234567890", ItemID: "5550001", ColumnValues: schedule},
		MapToUpdateItemColumnValuesInput(ItemSchedule{BoardID: request.BoardID, ItemID: "5550001", Request: request}))
	require.Equal(t, monday.CreateItemInput{BoardID: "1234567890", GroupID: "topics", ItemName: "Monthly Fire Drill Checklist - February", ColumnValues: schedule},
		MapToCreateItemInput(request))
	require.Equal(t, monday.AddUpdateInput{ItemID: "5550001", Body: "note"}, MapToAddUpdateInput(WorkUpdate{ItemID: "5550001", Body: "note"}))
	require.Equal(t, "Dex scheduled this item: status Working on it, due 2026-02-18.\n\nScheduled from the facilities request.",
		BuildUpdateBody(request, WorkItemScheduled))
	require.Equal(t, "Dex created this item: status Working on it, due 2026-02-18.\n\nScheduled from the facilities request.",
		BuildUpdateBody(request, WorkItemCreated))
}

func TestChooseOpenItemSkipsCompletedLookAlikeAndInactiveItems(t *testing.T) {
	request, err := BuildWorkRequest(validInput())
	require.NoError(t, err)
	status := func(text string) []monday.ItemColumnValue {
		return []monday.ItemColumnValue{{ID: "status", Type: monday.ColumnTypeStatus, Text: text}}
	}
	_, isFound := ChooseOpenItem(nil, request)
	require.False(t, isFound)
	chosen, isFound := ChooseOpenItem([]monday.Item{
		{ID: "1", Name: request.ItemName, State: monday.ItemStateActive, BoardID: request.BoardID, ColumnValues: status("Done")},
		{ID: "2", Name: request.ItemName + " (2025)", State: monday.ItemStateActive, BoardID: request.BoardID},
		{ID: "3", Name: request.ItemName, State: monday.ItemStateArchived, BoardID: request.BoardID},
		{ID: "4", Name: request.ItemName, State: monday.ItemStateActive, BoardID: "1111111111"},
		{ID: "5", Name: request.ItemName, State: monday.ItemStateActive, BoardID: request.BoardID, ColumnValues: status("Stuck")},
	}, request)
	require.True(t, isFound)
	require.Equal(t, "5", chosen.ID)
	require.True(t, IsOpenWorkItem(monday.Item{ID: "6", Name: request.ItemName}, request), "an item without a status is open")
	require.False(t, IsOpenWorkItem(monday.Item{ID: "7", Name: request.ItemName, ColumnValues: status(" done ")}, request))
}

func newUnitTestConnection(t *testing.T, connectionName string) monday.Connection {
	t.Helper()
	client, err := monday.New(monday.Config{}, sdkgo.StaticCredentialProvider[monday.Credentials]{})
	require.NoError(t, err)
	connection, err := monday.NewConnection(client, sdkgo.ConnectionRef{Provider: "monday", Name: connectionName})
	require.NoError(t, err)
	return connection
}
