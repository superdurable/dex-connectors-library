// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issueevents

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func createdEvent(action string, isTrashed bool) sdkgo.TriggerEvent[linear.IssueEvent] {
	return sdkgo.TriggerEvent[linear.IssueEvent]{
		ID: action + ":9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b:1790000000000", OccurredAt: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC),
		Payload: linear.IssueEvent{Action: action, Issue: linear.IssueEventIssue{IssueSummary: linear.IssueSummary{
			ID: "9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b", Identifier: "ENG-7", Title: "Fire panel wiring",
			Team: linear.TeamReference{ID: "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"},
		}, IsTrashed: isTrashed}},
	}
}

func TestAcceptIssueCreatedAdmitsOnlyLiveCreates(t *testing.T) {
	require.True(t, AcceptIssueCreated(createdEvent(linear.IssueEventActionCreate, false)))
	require.False(t, AcceptIssueCreated(createdEvent(linear.IssueEventActionUpdate, false)))
	require.False(t, AcceptIssueCreated(createdEvent(linear.IssueEventActionRemove, false)))
	require.False(t, AcceptIssueCreated(createdEvent(linear.IssueEventActionCreate, true)))
}

func TestResolveFlowIDAndInputCopyTheStableEvent(t *testing.T) {
	event := createdEvent(linear.IssueEventActionCreate, false)
	require.Equal(t, "linear-issue-create-9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b-1790000000000", ResolveFlowID(event))
	require.Equal(t, CreatedIssueEvent{
		EventID: event.ID, IssueID: "9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b", Identifier: "ENG-7", Title: "Fire panel wiring",
		TeamID: "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4", CreatedAt: event.OccurredAt,
	}, MapToFlowInput(event))
	require.Equal(t, linear.GetIssueInput{IssueID: "9e5f6a7b-8c9d-4e0f-8a1b-3c4d5e6f7a8b"}, MapToGetIssueInput(MapToFlowInput(event)))
}
