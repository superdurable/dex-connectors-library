// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetCardReadsTheDescriptionMembersAndListWithoutEmails(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{
			name: "[REQ-1042] Replace badge reader", due: "2026-10-15T17:00:00.000Z", labelIDs: []string{testLabelID}, members: true,
		}))
	})
	result, err := sdkgo.RunQuery(newTrelloDexContext("get-card"), newTrelloClient(t, provider.URL).GetCard(), trelloConnection,
		trello.GetCardInput{CardID: " " + testCardID + " "})
	require.NoError(t, err)
	require.Equal(t, trello.GetCardBranchFound, result.Branch)
	query, err := url.ParseQuery(provider.request(0).rawQuery)
	require.NoError(t, err)
	require.Contains(t, strings.Split(query.Get("fields"), ","), "desc")
	require.Equal(t, "true", query.Get("members"))
	require.Equal(t, "fullName,username", query.Get("member_fields"), "email addresses are never requested")
	require.Equal(t, "true", query.Get("list"))
	card := result.Value
	require.Equal(t, trello.Card{
		ID: testCardID, ShortLink: "LrrmgFyd", Name: "[REQ-1042] Replace badge reader", Description: "Badge reader at door 4 is offline.",
		BoardID: testBoardID, ListID: testListID, ListName: "Approved", Due: timePointer(time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)),
		LabelIDs: []string{testLabelID}, Labels: []trello.Label{{ID: testLabelID, Name: "Compliance", Color: "green"}},
		MemberIDs: []string{testMemberID}, Members: []trello.MemberReference{{ID: testMemberID, FullName: "Ada Lovelace", Username: "ada"}},
		Position: 16384, URL: "https://trello.com/c/LrrmgFyd/19-replace-badge-reader", ShortURL: "https://trello.com/c/LrrmgFyd",
		LastActivityAt: timePointer(time.Date(2026, 9, 30, 16, 15, 0, 0, time.UTC)),
	}, card)
	requireNoSentinel(t, result)
}

func TestGetCardBoundsTheDescriptionAndAcceptsLabelIDStrings(t *testing.T) {
	description := strings.Repeat("é", 16385)
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"`+testCardID+`","name":"Long","desc":"`+description+`","idBoard":"`+testBoardID+`",`+
			`"idList":"`+testListID+`","closed":false,"idLabels":["`+testLabelID+`"],"labels":["`+testLabelID+`","`+otherLabelID+`"],"idMembers":[]}`)
	})
	result, err := sdkgo.RunQuery(newTrelloDexContext("get-long"), newTrelloClient(t, provider.URL).GetCard(), trelloConnection,
		trello.GetCardInput{CardID: testCardID})
	require.NoError(t, err)
	require.Equal(t, trello.GetCardBranchFound, result.Branch)
	require.True(t, result.Value.IsDescriptionTruncated)
	require.Len(t, []rune(result.Value.Description), 16384)
	require.Equal(t, []string{testLabelID, otherLabelID}, result.Value.LabelIDs, "label IDs come from both fields without duplicates")
	require.Empty(t, result.Value.Labels)
}

func TestGetCardFlagsMoreThanFiftyLabels(t *testing.T) {
	var labelIDs []string
	for index := 0; index < 51; index++ {
		labelIDs = append(labelIDs, fmt.Sprintf(`"%s%04d"`, testLabelID[:20], index))
	}
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"`+testCardID+`","name":"Many","idBoard":"`+testBoardID+`","idList":"`+testListID+`",`+
			`"idLabels":[`+strings.Join(labelIDs, ",")+`]}`)
	})
	result, err := sdkgo.RunQuery(newTrelloDexContext("get-many-labels"), newTrelloClient(t, provider.URL).GetCard(), trelloConnection,
		trello.GetCardInput{CardID: testCardID})
	require.NoError(t, err)
	require.Len(t, result.Value.LabelIDs, 50)
	require.True(t, result.Value.HasMoreLabels)
}

func TestGetCardBranches(t *testing.T) {
	for name, test := range map[string]struct {
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
	}{
		"missing card": {func(response http.ResponseWriter) {
			writeText(t, response, http.StatusNotFound, "The requested resource was not found.")
		}, trello.GetCardBranchNotFound},
		"another card": {func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{id: oldestCardID}))
		}, trello.GetCardBranchInvalidResponse},
		"malformed due date": {func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{due: "SENTINEL"}))
		}, trello.GetCardBranchInvalidResponse},
		"array": {func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `[]`) }, trello.GetCardBranchInvalidResponse},
	} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
		result, err := sdkgo.RunQuery(newTrelloDexContext("get-branches"), newTrelloClient(t, provider.URL).GetCard(), trelloConnection,
			trello.GetCardInput{CardID: testCardID})
		require.NoError(t, err, name)
		require.Equal(t, test.branch, result.Branch, name)
		requireNoSentinel(t, result)
	}
	invalid, err := sdkgo.RunQuery(newTrelloDexContext("get-invalid"), newTrelloClient(t, closedLoopbackURL(t)).GetCard(), trelloConnection,
		trello.GetCardInput{CardID: "LrrmgFyd"})
	require.NoError(t, err)
	require.Equal(t, trello.GetCardBranchDefect, invalid.Branch, "a short link is not a card ID")
}

func timePointer(value time.Time) *time.Time { return &value }
