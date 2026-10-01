// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateCardSendsAbsoluteValuesAndConfirmsThemOnARead(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch request.Method {
		case http.MethodPut:
			writeJSON(t, response, http.StatusOK, `{"id":"`+testCardID+`"}`)
		default:
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{
				listID: otherListID, due: "2026-10-16T00:00:00.000Z", labelIDs: []string{testLabelID}, members: true,
			}))
		}
	})
	isClosed, due := false, time.Date(2026, 10, 15, 17, 0, 0, 0, time.FixedZone("PDT", -7*60*60))
	labelIDs, memberIDs := []string{testLabelID}, []string{testMemberID}
	result, err := sdkgo.RunMutation(newTrelloDexContext("update"), newTrelloClient(t, provider.URL).UpdateCard(), trelloConnection, trello.UpdateCardInput{
		CardID: testCardID, ListID: otherListID, Position: "top", IsClosed: &isClosed, Due: &due, LabelIDs: &labelIDs, MemberIDs: &memberIDs,
	})
	require.NoError(t, err)
	require.Equal(t, trello.UpdateCardBranchUpdated, result.Branch)
	require.Equal(t, otherListID, result.Value.Card.ListID)
	require.Equal(t, "Approved", result.Value.Card.ListName)
	require.Equal(t, 2, provider.requestCount(), "one PUT and one read-back")
	put := provider.request(0)
	require.Equal(t, http.MethodPut, put.method)
	require.Equal(t, "/cards/"+testCardID, put.path)
	require.Empty(t, put.rawQuery)
	require.Equal(t, "application/json", put.contentType)
	require.JSONEq(t, `{"idList":"`+otherListID+`","pos":"top","closed":false,"due":"2026-10-16T00:00:00.000Z",
		"idLabels":"`+testLabelID+`","idMembers":"`+testMemberID+`"}`, put.body)
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestUpdateCardClearsDueAndEveryLabelWithExplicitValues(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{}))
	})
	noLabels := []string{}
	isDueComplete := true
	result, err := sdkgo.RunMutation(newTrelloDexContext("update-clear"), newTrelloClient(t, provider.URL).UpdateCard(), trelloConnection,
		trello.UpdateCardInput{CardID: testCardID, ShouldClearDue: true, LabelIDs: &noLabels, IsDueComplete: &isDueComplete})
	require.NoError(t, err)
	require.JSONEq(t, `{"due":null,"idLabels":"","dueComplete":true}`, provider.request(0).body)
	require.Equal(t, trello.UpdateCardBranchInvalidResponse, result.Branch, "the read-back card is not due-complete")
	require.Equal(t, "Trello accepted the update but the card read back does not show the requested due-complete state", result.Failure.Message)
}

func TestUpdateCardAddsAndRemovesLabelsFromAFreshRead(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodPut:
			writeJSON(t, response, http.StatusOK, `{}`)
		case index == 0:
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{labelIDs: []string{otherLabelID}}))
		default:
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{listID: otherListID, labelIDs: []string{testLabelID}}))
		}
	})
	result, err := sdkgo.RunMutation(newTrelloDexContext("update-labels"), newTrelloClient(t, provider.URL).UpdateCard(), trelloConnection,
		trello.UpdateCardInput{CardID: testCardID, ListID: otherListID, AddLabelIDs: []string{testLabelID}, RemoveLabelIDs: []string{otherLabelID}})
	require.NoError(t, err)
	require.Equal(t, trello.UpdateCardBranchUpdated, result.Branch)
	require.Equal(t, 3, provider.requestCount(), "a read, the PUT, and the read-back")
	require.Equal(t, http.MethodGet, provider.request(0).method)
	require.JSONEq(t, `{"idList":"`+otherListID+`","idLabels":"`+testLabelID+`"}`, provider.request(1).body,
		"the PUT carries the complete resulting label set")
}

func TestUpdateCardRepeatsTheSameBodyWhenTheLabelWasAlreadyAdded(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{labelIDs: []string{testLabelID}}))
	})
	client := newTrelloClient(t, provider.URL)
	for attempt := 0; attempt < 2; attempt++ {
		result, err := sdkgo.RunMutation(newTrelloDexContext("update-repeat"), client.UpdateCard(), trelloConnection,
			trello.UpdateCardInput{CardID: testCardID, AddLabelIDs: []string{testLabelID}})
		require.NoError(t, err)
		require.Equal(t, trello.UpdateCardBranchUpdated, result.Branch)
	}
	require.JSONEq(t, provider.request(1).body, provider.request(4).body, "a repeated update sends the same absolute label set")
	require.JSONEq(t, `{"idLabels":"`+testLabelID+`"}`, provider.request(1).body)
}

func TestUpdateCardRetriesAmbiguousOutcomesAndMapsRejections(t *testing.T) {
	for name, test := range map[string]struct {
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
		retry  bool
	}{
		"server error":  {reply: func(response http.ResponseWriter) { writeText(t, response, http.StatusInternalServerError, "SENTINEL") }, retry: true},
		"lost response": {reply: func(response http.ResponseWriter) { dropConnection(t, response) }, retry: true},
		"rate limit": {reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusTooManyRequests, `{"error":"API_KEY_LIMIT_EXCEEDED"}`)
		}, retry: true},
		"missing card":  {reply: func(response http.ResponseWriter) { writeText(t, response, http.StatusNotFound, "SENTINEL") }, branch: trello.UpdateCardBranchNotFound},
		"invalid label": {reply: func(response http.ResponseWriter) { writeText(t, response, http.StatusBadRequest, "SENTINEL") }, branch: trello.UpdateCardBranchProviderRejected},
		"redirect": {reply: func(response http.ResponseWriter) {
			response.Header().Set("Location", "https://attacker.example/")
			response.WriteHeader(http.StatusFound)
		}, branch: trello.UpdateCardBranchInvalidResponse},
	} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
		isClosed := true
		result, err := sdkgo.RunMutation(newTrelloDexContext("update-branches"), newTrelloClient(t, provider.URL).UpdateCard(), trelloConnection,
			trello.UpdateCardInput{CardID: testCardID, IsClosed: &isClosed})
		if test.retry {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, name)
			continue
		}
		require.NoError(t, err, name)
		require.Equal(t, test.branch, result.Branch, name)
		require.Equal(t, testCardID, result.Value.CardID, name)
		require.Nil(t, result.Value.Card, name)
		requireNoSentinel(t, result)
	}
}

func TestUpdateCardDetectsAReadBackThatDoesNotShowTheChange(t *testing.T) {
	for name, test := range map[string]struct {
		input trello.UpdateCardInput
		field string
	}{
		"list":    {trello.UpdateCardInput{CardID: testCardID, ListID: otherListID}, "list"},
		"closed":  {trello.UpdateCardInput{CardID: testCardID, IsClosed: boolPointer(true)}, "archive state"},
		"due":     {trello.UpdateCardInput{CardID: testCardID, Due: timePointer(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))}, "due date"},
		"labels":  {trello.UpdateCardInput{CardID: testCardID, LabelIDs: &[]string{otherLabelID}}, "labels"},
		"members": {trello.UpdateCardInput{CardID: testCardID, MemberIDs: &[]string{}}, "members"},
	} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{labelIDs: []string{testLabelID}}))
		})
		result, err := sdkgo.RunMutation(newTrelloDexContext("update-unapplied"), newTrelloClient(t, provider.URL).UpdateCard(), trelloConnection, test.input)
		require.NoError(t, err, name)
		require.Equal(t, trello.UpdateCardBranchInvalidResponse, result.Branch, name)
		require.Equal(t, "Trello accepted the update but the card read back does not show the requested "+test.field, result.Failure.Message, name)
	}
}

func TestUpdateCardRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newTrelloClient(t, provider.URL)
	due := time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)
	for name, input := range map[string]trello.UpdateCardInput{
		"no change":            {CardID: testCardID},
		"invalid card":         {CardID: "LrrmgFyd", ListID: testListID},
		"board without list":   {CardID: testCardID, BoardID: testBoardID},
		"due set and cleared":  {CardID: testCardID, Due: &due, ShouldClearDue: true},
		"labels set and added": {CardID: testCardID, LabelIDs: &[]string{testLabelID}, AddLabelIDs: []string{otherLabelID}},
		"added and removed":    {CardID: testCardID, AddLabelIDs: []string{testLabelID}, RemoveLabelIDs: []string{testLabelID}},
		"invalid member":       {CardID: testCardID, MemberIDs: &[]string{"ada"}},
		"negative position":    {CardID: testCardID, Position: "-1"},
	} {
		result, err := sdkgo.RunMutation(newTrelloDexContext("update-invalid"), client.UpdateCard(), trelloConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, trello.UpdateCardBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
}

func boolPointer(value bool) *bool { return &value }
