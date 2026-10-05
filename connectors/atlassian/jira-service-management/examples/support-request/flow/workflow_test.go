// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supportrequest

import (
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBuildValidatedRequestPrefersThePickedDeskAndRequestType(t *testing.T) {
	request, err := BuildValidatedRequest(
		DeskSelection{ServiceDeskID: "10", ProjectKey: "ITH", ServiceDeskName: "IT Help"},
		RequestTypeSelection{ServiceDeskID: "10", RequestTypeID: "25", RequestTypeName: "Get IT help"},
		Input{RequesterEmail: " jane@acme.example.com ", Summary: "  Laptop will not boot ", ServiceDeskID: "11", ProjectKey: "HR", RequestTypeID: "30",
			InternalNote: " Check warranty. ", DestinationStatusName: " In progress "},
	)
	require.NoError(t, err)
	require.Equal(t, ValidatedRequest{
		ServiceDeskID: "10", ProjectKey: "ITH", RequestTypeID: "25", RequesterEmail: "jane@acme.example.com", Summary: "Laptop will not boot",
		InternalNote: "Check warranty.", DestinationStatusName: "In progress",
	}, request)

	request, err = BuildValidatedRequest(DeskSelection{}, RequestTypeSelection{}, Input{
		RequesterEmail: "jane@acme.example.com", Summary: "Laptop", ServiceDeskID: "11", ProjectKey: "HR", RequestTypeID: "30",
	})
	require.NoError(t, err)
	require.Equal(t, "11", request.ServiceDeskID)
	require.Equal(t, "30", request.RequestTypeID)

	for _, input := range []Input{
		{RequesterEmail: "jane@acme.example.com", Summary: "Laptop", RequestTypeID: "30"},
		{RequesterEmail: "jane@acme.example.com", Summary: "Laptop", ServiceDeskID: "11", ProjectKey: "HR"},
		{Summary: "Laptop", ServiceDeskID: "11", ProjectKey: "HR", RequestTypeID: "30"},
		{RequesterEmail: "jane@acme.example.com", Summary: "one\ntwo", ServiceDeskID: "11", ProjectKey: "HR", RequestTypeID: "30"},
	} {
		_, err := BuildValidatedRequest(DeskSelection{}, RequestTypeSelection{}, input)
		require.Error(t, err, "%+v", input)
	}
}

func TestValidateSelectionsRejectsARequestTypeFromAnotherDesk(t *testing.T) {
	require.NoError(t, ValidateSelections(DeskSelection{ServiceDeskID: "10"}, RequestTypeSelection{ServiceDeskID: "10"}))
	require.NoError(t, ValidateSelections(DeskSelection{}, RequestTypeSelection{ServiceDeskID: "10"}))
	require.Error(t, ValidateSelections(DeskSelection{ServiceDeskID: "10"}, RequestTypeSelection{ServiceDeskID: "11"}))
}

func TestFindSameSummaryTicketRejectsNearDuplicatesAndDoneRequests(t *testing.T) {
	tickets := []jiraservicemanagement.Ticket{
		{Key: "ITH-1", Summary: "Laptop will not boot (again)", Status: jiraservicemanagement.TicketStatus{CategoryKey: jiraservicemanagement.StatusCategoryToDo}},
		{Key: "ITH-2", Summary: "laptop will not boot", Status: jiraservicemanagement.TicketStatus{CategoryKey: jiraservicemanagement.StatusCategoryDone}},
		{Key: "ITH-3", Summary: " LAPTOP WILL NOT BOOT ", Status: jiraservicemanagement.TicketStatus{CategoryKey: jiraservicemanagement.StatusCategoryInProgress}},
	}
	ticket, isFound := FindSameSummaryTicket(tickets, "Laptop will not boot")
	require.True(t, isFound)
	require.Equal(t, "ITH-3", ticket.Key)
	_, isFound = FindSameSummaryTicket(tickets[:2], "Laptop will not boot")
	require.False(t, isFound)
}

func TestMappersSendInternalNotesAndPublicRepliesWithTheirVisibility(t *testing.T) {
	require.False(t, MapToInternalNoteInput(CommentRequest{IssueKey: "ITH-1", Body: "note"}).IsPublic)
	require.True(t, MapToPublicReplyInput(CommentRequest{IssueKey: "ITH-1", Body: "reply"}).IsPublic)
	search := MapToSearchTicketsInput(DuplicateSearch{ProjectKey: "ITH", RequesterAccountID: "qm:customer", Summary: "Laptop will not boot"})
	jql, err := jiraservicemanagement.BuildTicketSearchJQL(search)
	require.NoError(t, err)
	require.Equal(t, `project = "ITH" AND statusCategory in (2, 4) AND reporter in ("qm:customer") AND summary ~ "\"Laptop will not boot\"" ORDER BY created DESC`, jql)
	require.Equal(t, jiraservicemanagement.CreateTicketInput{
		ServiceDeskID: "10", RequestTypeID: "25", Summary: "Laptop", Description: "Boot loop.", RaiseOnBehalfOfAccountID: "qm:customer",
	}, MapToCreateTicketInput(SupportRequest{
		Request:            ValidatedRequest{ServiceDeskID: "10", RequestTypeID: "25", Summary: "Laptop", Description: "Boot loop."},
		RequesterAccountID: "qm:customer",
	}))
}

func TestFindActiveCustomerSkipsInactiveAccounts(t *testing.T) {
	customer, isFound := FindActiveCustomer([]jiraservicemanagement.Customer{{AccountID: "qm:old"}, {AccountID: "qm:new", IsActive: true}})
	require.True(t, isFound)
	require.Equal(t, "qm:new", customer.AccountID)
	_, isFound = FindActiveCustomer([]jiraservicemanagement.Customer{{AccountID: "qm:old"}})
	require.False(t, isFound)
}

func TestDescribeSLAsMarksRunningPausedAndBreachedCycles(t *testing.T) {
	require.Equal(t, []string{"Time to first response", "Time to resolution (paused, breached)", "Time to close (running)"}, DescribeSLAs([]jiraservicemanagement.TicketSLA{
		{Name: "Time to first response"},
		{Name: "Time to resolution", OngoingCycle: &jiraservicemanagement.SLACycle{IsPaused: true, IsBreached: true}},
		{Name: "Time to close", OngoingCycle: &jiraservicemanagement.SLACycle{}},
	}))
}

func TestReportedRequestKeysAreValidated(t *testing.T) {
	for _, key := range []string{"ITH-42", "A1_B-9"} {
		require.True(t, isRequestKey(key), key)
	}
	for _, key := range []string{"", "ITH", "ITH-0", "ith-1", "1TH-1", "ITH-1-2", "ITH-12345678901234567890"} {
		require.False(t, isRequestKey(key), key)
	}
}

func TestFlowRegistersEveryStepAndRPC(t *testing.T) {
	client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: "11223344-a1b2-4b33-8c44-def123456789"},
		sdkgo.StaticCredentialProvider[jiraservicemanagement.Credentials]{})
	require.NoError(t, err)
	connection, err := jiraservicemanagement.NewConnection(client, sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection, DeskSelection{ServiceDeskID: "10", ProjectKey: "ITH"}, RequestTypeSelection{RequestTypeID: "25"})
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Len(t, flow.GetRPCs(), 5)
}
