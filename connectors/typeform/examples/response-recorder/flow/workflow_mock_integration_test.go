//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package responserecorder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/connectors/typeform/typeformmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestResponseRecorderExampleRunsOnTypeformMockWithRealDex(t *testing.T) {
	mock := typeformmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()

	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := FlowIDPrefix + "mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	submission := Submission{
		EventID: "f_signup_1:response_ada_1", FormID: "f_signup_1", ResponseToken: "response_ada_1",
		SubmittedAt: time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC),
		Answers: []typeform.FormAnswer{
			{FieldID: "field_email", FieldType: "email", Type: typeform.AnswerTypeEmail, Email: "ada@example.test"},
			{FieldID: "field_retired", FieldType: "short_text", Type: typeform.AnswerTypeText, Text: "old answer"},
		},
		HiddenFields: map[string]string{"campaign": "autumn"},
	}
	_, err = client.StartFlow(ctx, flow, flowID, submission, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var recorded RecordedQuestions
	require.NoError(t, result.DecodeSingleOutput(&recorded))
	require.Equal(t, typeform.GetFormBranchFound, recorded.Branch)
	require.Len(t, recorded.Questions, 2)
	require.Equal(t, "field_name", recorded.Questions[0].Field.ID)
	require.Nil(t, recorded.Questions[0].Answer, "the skipped name question remains visible")
	require.Equal(t, "field_email", recorded.Questions[1].Field.ID)
	require.Equal(t, "ada@example.test", recorded.Questions[1].Answer.Email)
	require.Equal(t, submission.Answers[1:], recorded.UnmatchedAnswers)
	calls := mock.GetForm().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, typeform.GetFormInput{FormID: "f_signup_1"}, calls[0].Input)
	require.Equal(t, typeform.GetFormBranchFound, calls[0].Branch)
}
