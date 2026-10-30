//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supportrequest

import (
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management/go"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management/go/jiraservicemanagementmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSupportRequestUsesMocksAcrossADexRetry(t *testing.T) {
	mock := jiraservicemanagementmock.New(t, ConnectionName)
	mock.CreateTicket().Respond(
		jiraservicemanagementmock.CreateTicketRetry(sdkgo.Failure{Kind: sdkgo.FailureAvailability, Message: "Jira returned 503"}, 0),
		jiraservicemanagementmock.CreateTicketDefault(),
	)
	mock.AddComment().Respond(
		jiraservicemanagementmock.AddCommentDefault(),
		jiraservicemanagementmock.AddCommentAdded(jiraservicemanagement.AddCommentOutput{
			IssueIDOrKey: "ITH-42", CommentID: "1002", IsPublic: true,
		}, nil),
	)
	flow, harness := newSupportIntegrationHarnessForConnection(t, mock.Connection())
	ctx := integrationContext(t)
	input := supportInput("Laptop will not boot")
	record := runSupportFlow(t, ctx, harness, flow, "mock-retry", input)
	require.Equal(t, PhaseHandled, record.Phase)
	require.Equal(t, input.Summary, record.Request.Summary)
	require.Equal(t, input.Labels, record.Labels)
	require.Equal(t, input.DestinationStatusName, record.Status.Name)
	require.Equal(t, 1, record.CreateAttempts, "a Dex retry stays in one create Step execution")

	findCalls := mock.FindCustomerByEmail().Calls()
	require.Len(t, findCalls, 1)
	require.Equal(t, jiraservicemanagement.FindCustomerByEmailInput{ServiceDeskID: "10", Email: "jane@acme.example.com"}, findCalls[0].Input)
	searchCalls := mock.SearchTickets().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, jiraservicemanagement.SearchTicketsInput{
		ProjectKey: "ITH", ReporterAccountIDs: []string{"5b10ac8d82e05b22cc7d4ef5"}, SummaryPhrase: "Laptop will not boot",
		StatusCategories: []jiraservicemanagement.StatusCategoryKey{jiraservicemanagement.StatusCategoryToDo, jiraservicemanagement.StatusCategoryInProgress},
		PageSize:         20,
	}, searchCalls[0].Input)
	createCalls := mock.CreateTicket().Calls()
	require.Len(t, createCalls, 2)
	expectedCreate := jiraservicemanagement.CreateTicketInput{
		ServiceDeskID: "10", RequestTypeID: "25", Summary: "Laptop will not boot",
		Description: "My laptop will not boot.\nError 0x7B.", RaiseOnBehalfOfAccountID: "5b10ac8d82e05b22cc7d4ef5",
	}
	require.Equal(t, expectedCreate, createCalls[0].Input)
	require.Equal(t, expectedCreate, createCalls[1].Input)
	require.True(t, createCalls[0].IsRetry)
	require.Equal(t, createCalls[0].CallID, createCalls[1].CallID)
	require.Equal(t, createCalls[0].IdempotencyKey, createCalls[1].IdempotencyKey)
	require.Equal(t, jiraservicemanagement.CreateTicketBranchCreated, createCalls[1].Branch)
	getCalls := mock.GetTicket().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, jiraservicemanagement.GetTicketInput{IssueIDOrKey: "ITH-42"}, getCalls[0].Input)
	updateCalls := mock.UpdateTicket().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, jiraservicemanagement.UpdateTicketInput{IssueIDOrKey: "ITH-42", AddLabels: []string{"hardware"}, PriorityName: "High"}, updateCalls[0].Input)
	commentCalls := mock.AddComment().Calls()
	require.Len(t, commentCalls, 2)
	require.Equal(t, jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "Check the warranty first."}, commentCalls[0].Input)
	require.Equal(t, jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "We are looking into it.", IsPublic: true}, commentCalls[1].Input)
	transitionCalls := mock.TransitionTicket().Calls()
	require.Len(t, transitionCalls, 1)
	require.Equal(t, jiraservicemanagement.TransitionTicketInput{IssueIDOrKey: "ITH-42", DestinationStatusName: "In progress"}, transitionCalls[0].Input)
}
