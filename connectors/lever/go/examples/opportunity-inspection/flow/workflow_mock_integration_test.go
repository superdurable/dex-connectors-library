//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opportunityinspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/lever/go"
	"github.com/superdurable/dex-connectors-library/connectors/lever/go/levermock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestOpportunityInspectionUsesDefaultMockWithRealDex(t *testing.T) {
	mock := levermock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	postingFlow := NewPostingListFlow(mock.Connection())
	noteFlow := NewNoteListFlow(mock.Connection())
	interviewFlow := NewInterviewListFlow(mock.Connection())
	userFlow := NewUserListFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, postingFlow, noteFlow, interviewFlow, userFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "lever-opportunityinspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, lever.GetOpportunityInput{OpportunityID: "250d8f03-738a-4bba-a671-8a3d73477145"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output lever.OpportunityStage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "250d8f03-738a-4bba-a671-8a3d73477145", output.ID)
	require.Equal(t, "Sample Candidate", output.Name)
	require.Equal(t, "00922a60-7c15-422b-b086-f62000824fd7", output.Stage)
	require.Len(t, mock.GetOpportunity().Calls(), 1)
	require.Len(t, mock.ListOpportunities().Calls(), 1)
	postingFlowID := "lever-postings-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, postingFlow, postingFlowID, lever.ListPostingsInput{State: "draft"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	postingResult, err := client.WaitForFlow(ctx, postingFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, postingResult.Status, postingResult.ErrorMessage)
	var postings lever.PostingsPage
	require.NoError(t, postingResult.DecodeSingleOutput(&postings))
	require.Equal(t, "f2f01e16-27f8-4711-a728-7d49499795a0", postings.Postings[0].ID)
	require.Len(t, mock.ListPostings().Calls(), 1)
	noteFlowID := "lever-notes-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, noteFlow, noteFlowID, lever.ListNotesInput{OpportunityID: "250d8f03-738a-4bba-a671-8a3d73477145"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	noteResult, err := client.WaitForFlow(ctx, noteFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, noteResult.Status, noteResult.ErrorMessage)
	var notes lever.NotesPage
	require.NoError(t, noteResult.DecodeSingleOutput(&notes))
	require.Equal(t, "b1dbbcfe-281c-46a0-8f51-57049158d3f3", notes.Notes[0].ID)
	require.Len(t, mock.ListNotes().Calls(), 1)
	interviewFlowID := "lever-interviews-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, interviewFlow, interviewFlowID, lever.ListInterviewsInput{OpportunityID: "250d8f03-738a-4bba-a671-8a3d73477145"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	interviewResult, err := client.WaitForFlow(ctx, interviewFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, interviewResult.Status, interviewResult.ErrorMessage)
	var interviews lever.InterviewsPage
	require.NoError(t, interviewResult.DecodeSingleOutput(&interviews))
	require.Equal(t, "6ff55c8e-fe04-4eb4-835a-630b1c0da421", interviews.Interviews[0].ID)
	require.Len(t, mock.ListInterviews().Calls(), 1)
	userFlowID := "lever-users-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, userFlow, userFlowID, lever.ListUsersInput{Email: "sample@example.com", IncludeDeactivated: true}, dex.StartFlowOptions{})
	require.NoError(t, err)
	userResult, err := client.WaitForFlow(ctx, userFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, userResult.Status, userResult.ErrorMessage)
	var users lever.UsersPage
	require.NoError(t, userResult.DecodeSingleOutput(&users))
	require.Equal(t, "8d49b010-cc6a-4f40-ace5-e86061c677ed", users.Users[0].ID)
	require.Len(t, mock.ListUsers().Calls(), 1)
}
