//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedrequestcard

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/trellomock"
)

func TestApprovedRequestUsesTrelloMockWithRealDex(t *testing.T) {
	mock := trellomock.New(t, ConnectionName)
	harness := newTrelloHarnessForConnection(t, mock.Connection(), DuplicateCheck{})
	record := harness.runRequestFlow(t, "mock-approved", requestInput("REQ-1042", "Replace badge reader"))
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, "[REQ-1042] Replace badge reader", record.Request.CardName)
	require.Equal(t, "Approved by Grace Hopper.\n\nBudget code FAC-7.", record.Request.Comment)
	require.Equal(t, 3, record.OpenCardPagesRead)
	require.Equal(t, 1, record.CreateAttempts)
	require.False(t, record.IsExistingCardReused)

	listCalls := mock.ListCards().Calls()
	require.Len(t, listCalls, 3)
	require.Equal(t, trello.ListCardsInput{BoardID: integrationBoardID, Status: trello.CardStatusOpen, PageSize: 100}, listCalls[0].Input)
	require.Equal(t, "6512f0a1c2d3e4f5a6b71001", listCalls[1].Input.Before)
	require.Equal(t, "6512f0a1c2d3e4f5a6b71003", listCalls[2].Input.Before)
	createCalls := mock.CreateCard().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, trello.CreateCardInput{
		ListID: approvedListID, Name: "[REQ-1042] Replace badge reader",
		Description: "Badge reader at door 4 is offline.\nFacilities approved the spend.",
		Due:         &integrationDue, LabelIDs: []string{complianceLabelID}, MemberIDs: []string{adaMemberID}, Position: "top",
	}, createCalls[0].Input)
	commentCalls := mock.AddComment().Calls()
	require.Len(t, commentCalls, 1)
	require.Equal(t, trello.AddCommentInput{
		CardID: "6512f0a1c2d3e4f5a6b72001", Text: "Approved by Grace Hopper.\n\nBudget code FAC-7.",
	}, commentCalls[0].Input)
	require.Empty(t, mock.GetCard().Calls())
	require.Empty(t, mock.UpdateCard().Calls())
}
