// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	newestCardID = "6512f0a1c2d3e4f5a6b7c903"
	middleCardID = "6512f0a1c2d3e4f5a6b7c902"
	oldestCardID = "6512f0a1c2d3e4f5a6b7c901"
)

func TestListCardsReadsABoardPageNewestFirstWithoutDescriptions(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, "["+strings.Join([]string{
			cardJSON(cardJSONOptions{id: newestCardID, name: "[REQ-1042] Replace badge reader", due: "2026-10-15T17:00:00.000Z", labelIDs: []string{testLabelID}}),
			cardJSON(cardJSONOptions{id: oldestCardID, name: "Follow-up"}),
			cardJSON(cardJSONOptions{id: middleCardID, name: "Paint the lobby"}),
		}, ",")+"]")
	})
	result, err := sdkgo.RunQuery(newTrelloDexContext("list-board"), newTrelloClient(t, provider.URL).ListCards(), trelloConnection,
		trello.ListCardsInput{BoardID: testBoardID, PageSize: 3})
	require.NoError(t, err)
	require.Equal(t, trello.ListCardsBranchListed, result.Branch)
	request := provider.request(0)
	require.Equal(t, "/boards/"+testBoardID+"/cards", request.path)
	query, err := url.ParseQuery(request.rawQuery)
	require.NoError(t, err)
	require.Equal(t, "open", query.Get("filter"), "a blank status lists open cards")
	require.Equal(t, "3", query.Get("limit"))
	require.Equal(t, "-id", query.Get("sort"))
	require.Empty(t, query.Get("before"))
	require.NotContains(t, strings.Split(query.Get("fields"), ","), "desc", "list pages stay small without descriptions")
	require.Len(t, result.Value.Cards, 3)
	require.Equal(t, 3, result.Value.ScannedCardCount)
	require.Equal(t, oldestCardID, result.Value.NextBefore, "a full page continues before its oldest card")
	first := result.Value.Cards[0]
	require.Equal(t, newestCardID, first.ID)
	require.Equal(t, testBoardID, first.BoardID)
	require.Equal(t, testListID, first.ListID)
	require.Equal(t, time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC), *first.Due)
	require.Equal(t, []string{testLabelID}, first.LabelIDs)
	require.Equal(t, []trello.Label{{ID: testLabelID, Name: "Compliance", Color: "green"}}, first.Labels)
	require.Empty(t, first.Description)
	require.Empty(t, first.Members)
	require.True(t, first.HasLabel(strings.ToUpper(testLabelID)))
}

func TestListCardsContinuesAListWithItsCursorAndStatus(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, "["+cardJSON(cardJSONOptions{id: oldestCardID, name: "Archived", isClosed: true})+"]")
	})
	result, err := sdkgo.RunQuery(newTrelloDexContext("list-list"), newTrelloClient(t, provider.URL).ListCards(), trelloConnection,
		trello.ListCardsInput{ListID: testListID, Status: trello.CardStatusClosed, Before: middleCardID})
	require.NoError(t, err)
	require.Equal(t, trello.ListCardsBranchListed, result.Branch)
	request := provider.request(0)
	require.Equal(t, "/lists/"+testListID+"/cards", request.path)
	query, err := url.ParseQuery(request.rawQuery)
	require.NoError(t, err)
	require.Equal(t, "closed", query.Get("filter"))
	require.Equal(t, "50", query.Get("limit"), "zero requests 50 cards")
	require.Equal(t, middleCardID, query.Get("before"))
	require.Empty(t, result.Value.NextBefore, "a short page is the last page")
	require.True(t, result.Value.Cards[0].IsClosed)
}

func TestListCardsAppliesDueFiltersToEachPage(t *testing.T) {
	page := "[" + strings.Join([]string{
		cardJSON(cardJSONOptions{id: newestCardID, name: "Due this week", due: "2026-10-15T17:00:00.000Z"}),
		cardJSON(cardJSONOptions{id: middleCardID, name: "Due next month", due: "2026-11-20T17:00:00.000Z"}),
		cardJSON(cardJSONOptions{id: oldestCardID, name: "No due date"}),
	}, ",") + "]"
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, page)
	})
	client := newTrelloClient(t, provider.URL)
	dueAfter, dueBefore := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	isTrue, isFalse := true, false
	for name, test := range map[string]struct {
		input   trello.ListCardsInput
		cardIDs []string
	}{
		"due in October":      {trello.ListCardsInput{DueAfter: &dueAfter, DueBefore: &dueBefore}, []string{newestCardID}},
		"due before November": {trello.ListCardsInput{DueBefore: &dueBefore}, []string{newestCardID}},
		"due from November":   {trello.ListCardsInput{DueAfter: &dueBefore}, []string{middleCardID}},
		"with a due date":     {trello.ListCardsInput{HasDueDate: &isTrue}, []string{newestCardID, middleCardID}},
		"without a due date":  {trello.ListCardsInput{HasDueDate: &isFalse}, []string{oldestCardID}},
		"due not complete":    {trello.ListCardsInput{IsDueComplete: &isFalse}, []string{newestCardID, middleCardID, oldestCardID}},
		"due complete":        {trello.ListCardsInput{IsDueComplete: &isTrue}, nil},
	} {
		input := test.input
		input.BoardID, input.PageSize = testBoardID, 3
		result, err := sdkgo.RunQuery(newTrelloDexContext("list-due"), client.ListCards(), trelloConnection, input)
		require.NoError(t, err, name)
		var cardIDs []string
		for _, card := range result.Value.Cards {
			cardIDs = append(cardIDs, card.ID)
		}
		require.Equal(t, test.cardIDs, cardIDs, name)
		require.Equal(t, 3, result.Value.ScannedCardCount, name)
		require.Equal(t, oldestCardID, result.Value.NextBefore, "%s: a filtered page still continues", name)
	}
}

func TestListCardsBranches(t *testing.T) {
	for name, test := range map[string]struct {
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
	}{
		"missing board": {func(response http.ResponseWriter) {
			writeText(t, response, http.StatusNotFound, "The requested resource was not found. SENTINEL")
		}, trello.ListCardsBranchNotFound},
		"invalid token": {func(response http.ResponseWriter) {
			writeText(t, response, http.StatusUnauthorized, "invalid token SENTINEL")
		}, trello.ListCardsBranchProviderRejected},
		"object instead of array": {func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"cards":[]}`)
		}, trello.ListCardsBranchInvalidResponse},
		"more cards than requested": {func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, "["+cardJSON(cardJSONOptions{id: newestCardID})+","+cardJSON(cardJSONOptions{id: oldestCardID})+"]")
		}, trello.ListCardsBranchInvalidResponse},
		"card without a list": {func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `[{"id":"`+testCardID+`","name":"SENTINEL","idBoard":"`+testBoardID+`"}]`)
		}, trello.ListCardsBranchInvalidResponse},
	} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
		result, err := sdkgo.RunQuery(newTrelloDexContext("list-branches"), newTrelloClient(t, provider.URL).ListCards(), trelloConnection,
			trello.ListCardsInput{BoardID: testBoardID, PageSize: 1})
		require.NoError(t, err, name)
		require.Equal(t, test.branch, result.Branch, name)
		requireNoSentinel(t, result)
	}
}

func TestListCardsRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newTrelloClient(t, provider.URL)
	dueBefore := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	isFalse := false
	for name, input := range map[string]trello.ListCardsInput{
		"neither container":   {},
		"both containers":     {BoardID: testBoardID, ListID: testListID},
		"short link":          {BoardID: "LrrmgFyd"},
		"unknown status":      {BoardID: testBoardID, Status: "visible"},
		"page too large":      {BoardID: testBoardID, PageSize: 101},
		"negative page":       {BoardID: testBoardID, PageSize: -1},
		"cursor is not an ID": {BoardID: testBoardID, Before: "2026-10-01"},
		"range without due":   {BoardID: testBoardID, DueBefore: &dueBefore, HasDueDate: &isFalse},
		"empty due range":     {BoardID: testBoardID, DueBefore: &dueBefore, DueAfter: &dueBefore},
	} {
		result, err := sdkgo.RunQuery(newTrelloDexContext("list-invalid"), client.ListCards(), trelloConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, trello.ListCardsBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
}
