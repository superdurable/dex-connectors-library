// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedrequestcard

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	unitBoardID = "6512f0a1c2d3e4f5a6b70001"
	unitListID  = "6512f0a1c2d3e4f5a6b70102"
	unitLabelID = "6512f0a1c2d3e4f5a6b70201"
	unitCardID  = "6512f0a1c2d3e4f5a6b7c901"
)

func TestBuildCardRequestNamesTheCardAndWritesTheApprovalComment(t *testing.T) {
	due := time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)
	input := Input{
		RequestID: " REQ-1042 ", Title: " Replace badge reader ", Details: "Door 4 is offline.", ApprovedBy: " Grace Hopper ",
		ApprovalNote: " Budget approved. ", BoardID: " " + unitBoardID + " ", ListID: unitListID, DueAt: &due,
		LabelIDs: []string{" " + unitLabelID}, MemberIDs: []string{unitCardID},
	}
	request, err := BuildCardRequest(input)
	require.NoError(t, err)
	require.Equal(t, CardRequest{
		RequestID: "REQ-1042", CardName: "[REQ-1042] Replace badge reader", Description: "Door 4 is offline.",
		BoardID: unitBoardID, ListID: unitListID, Due: &due, LabelIDs: []string{unitLabelID}, MemberIDs: []string{unitCardID},
		Comment: "Approved by Grace Hopper.\n\nBudget approved.",
	}, request)

	valid := Input{RequestID: "REQ-1042", Title: "Replace badge reader", ApprovedBy: "Grace", BoardID: unitBoardID, ListID: unitListID}
	for name, change := range map[string]func(*Input){
		"missing request ID": func(input *Input) { input.RequestID = "" },
		"spaced request ID":  func(input *Input) { input.RequestID = "REQ 1042" },
		"two-line title":     func(input *Input) { input.Title = "one\ntwo" },
		"missing approver":   func(input *Input) { input.ApprovedBy = "" },
		"board short link":   func(input *Input) { input.BoardID = "Fqd6NosI" },
		"missing list":       func(input *Input) { input.ListID = "" },
		"label name":         func(input *Input) { input.LabelIDs = []string{"Compliance"} },
	} {
		invalid := valid
		change(&invalid)
		_, err := BuildCardRequest(invalid)
		require.Error(t, err, name)
	}
}

func TestFindRequestCardMatchesOnlyTheBracketedRequestIDOfAnOpenCard(t *testing.T) {
	cards := []trello.Card{
		{ID: "1", Name: "[REQ-10420] Replace badge reader"},
		{ID: "2", Name: "Follow-up on REQ-1042"},
		{ID: "3", Name: "[REQ-1042] Replace badge reader", IsClosed: true},
		{ID: "4", Name: " [REQ-1042] Replace badge reader (reopened)"},
	}
	card, isFound := FindRequestCard(cards, "REQ-1042")
	require.True(t, isFound)
	require.Equal(t, "4", card.ID)
	_, isFound = FindRequestCard(cards[:3], "REQ-1042")
	require.False(t, isFound)
}

func TestMappersBuildTheConnectorInputs(t *testing.T) {
	due := time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)
	request := CardRequest{
		RequestID: "REQ-1042", CardName: "[REQ-1042] Replace badge reader", Description: "Door 4.", BoardID: unitBoardID, ListID: unitListID,
		Due: &due, LabelIDs: []string{unitLabelID}, MemberIDs: []string{unitCardID}, Comment: "Approved by Grace.",
	}
	require.Equal(t, trello.ListCardsInput{BoardID: unitBoardID, Status: trello.CardStatusOpen, PageSize: 100, Before: unitCardID},
		MapToListCardsInput(OpenCardPage{BoardID: unitBoardID, PageSize: 100, Before: unitCardID}))
	require.Equal(t, trello.CreateCardInput{
		ListID: unitListID, Name: "[REQ-1042] Replace badge reader", Description: "Door 4.", Due: &due,
		LabelIDs: []string{unitLabelID}, MemberIDs: []string{unitCardID}, Position: "top",
	}, MapToCreateCardInput(request))
	isClosed := false
	require.Equal(t, trello.UpdateCardInput{CardID: unitCardID, ListID: unitListID, IsClosed: &isClosed, Due: &due, AddLabelIDs: []string{unitLabelID}},
		MapToUpdateCardInput(CardMove{CardID: unitCardID, ListID: unitListID, Due: &due, LabelIDs: []string{unitLabelID}}))
	require.Nil(t, MapToUpdateCardInput(CardMove{CardID: unitCardID, ListID: unitListID}).Due, "a blank due date leaves the card's own")
	require.Equal(t, trello.AddCommentInput{CardID: unitCardID, Text: "Approved by Grace."},
		MapToAddCommentInput(CommentRequest{CardID: unitCardID, Text: "Approved by Grace."}))
}

func TestDuplicateCheckDefaultsBoundTheSearch(t *testing.T) {
	require.Equal(t, DuplicateCheck{PageSize: 100, MaximumPages: 5}, NewFlow(trello.Connection{}, DuplicateCheck{}).duplicateCheck)
	require.Equal(t, DuplicateCheck{PageSize: 100, MaximumPages: 2}, NewFlow(trello.Connection{}, DuplicateCheck{PageSize: 500, MaximumPages: 2}).duplicateCheck)
	require.Equal(t, DuplicateCheck{PageSize: 2, MaximumPages: 5}, NewFlow(trello.Connection{}, DuplicateCheck{PageSize: 2}).duplicateCheck)
}

func TestReportedCardIDsAreValidated(t *testing.T) {
	require.True(t, isTrelloID(unitCardID))
	for _, value := range []string{"", "LrrmgFyd", "https://trello.com/c/LrrmgFyd", "6512f0a1c2d3e4f5a6b7c90g"} {
		require.False(t, isTrelloID(value), value)
	}
}

func TestFlowRegistersEveryStepAndRPC(t *testing.T) {
	client, err := trello.New(trello.Config{}, sdkgo.StaticCredentialProvider[trello.Credentials]{})
	require.NoError(t, err)
	connection, err := trello.NewConnection(client, sdkgo.ConnectionRef{Provider: "trello", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection, DuplicateCheck{})
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Len(t, flow.GetRPCs(), 5)
}
