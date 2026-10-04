//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	responserecorder "github.com/superdurable/dex-connectors-library/connectors/typeform/examples/response-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestSignedSubmissionStartsOneFlowAndDuplicatesAndForgeriesStartNoneWithRealDex covers README steps 5 and 6.
func TestSignedSubmissionStartsOneFlowAndDuplicatesAndForgeriesStartNoneWithRealDex(t *testing.T) {
	fake := newFakeTypeform(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, fake.connectionOption(), typeform.WithLogger(logs.logger()))
	flow := responserecorder.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newSubmissionEndpoint(t, connection, newSubmissionTarget(client, flow, logs.logger()))
	endpoint.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	token := uniqueToken("signed")
	eventID := recordedFormID + ":" + token
	body := submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, token, true)

	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, body, sentinelSecret, false))
	recorded := waitForRecorded(t, ctx, client, eventID)
	require.Equal(t, typeform.GetFormBranchFound, recorded.Branch)
	require.Empty(t, recorded.UnmatchedAnswers)
	answersByRef := map[string]*typeform.FormAnswer{}
	for _, question := range recorded.Questions {
		answersByRef[question.Field.Ref] = question.Answer
	}
	require.Equal(t, []string{"first_name", "email", "city", "consent"}, questionRefs(recorded), "every question in form order")
	require.Equal(t, &typeform.FormAnswer{FieldID: "SMEUb7VJz92Q", FieldRef: "email", FieldType: "email", FieldTitle: "Your email?",
		Type: typeform.AnswerTypeEmail, Email: "ada@example.com"}, answersByRef["email"])
	require.Equal(t, &typeform.FormAnswerChoice{ID: "4WIlUvKOl0UB", Label: "London", Ref: "london"}, answersByRef["city"].Choice)
	require.Equal(t, "first_name", answersByRef["first_name"].FieldRef, "a ref the answer omits comes from the webhook's definition")
	require.Nil(t, answersByRef["consent"], "a skipped question is recorded without an answer")
	require.Equal(t, 1, fake.readCount(recordedFormID), "the Flow read the form once")

	// Typeform redelivers the same submission: the Flow start deduplicates it.
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, body, sentinelSecret, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event delivered", map[string]string{"event_id": eventID, "duplicate": "true"})) == 1
	}, 20*time.Second, 25*time.Millisecond, "the redelivery reaches Dex as a duplicate start")
	require.Len(t, logs.find("trigger event delivered", map[string]string{"event_id": eventID, "duplicate": "false"}), 1)
	require.Equal(t, 1, fake.readCount(recordedFormID), "no second Flow read the form")

	// A forged delivery answers 400, and an empty submission is filtered by the application; neither starts a Flow.
	forgedToken, emptyToken, laterToken := uniqueToken("forged"), uniqueToken("empty"), uniqueToken("later")
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, forgedToken, true), sentinelSecret, true))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, forgedToken, true), "not-the-webhook-secret", false))
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, emptyToken, false), sentinelSecret, false))
	require.Eventually(t, func() bool {
		return len(logs.find("trigger event skipped: filtered", map[string]string{"event_id": recordedFormID + ":" + emptyToken})) == 1
	}, 20*time.Second, 25*time.Millisecond, "AcceptSubmission consumes a submission without answers")
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, laterToken, true), sentinelSecret, false))
	waitForRecorded(t, ctx, client, recordedFormID+":"+laterToken)
	requireNoFlow(t, ctx, client, recordedFormID+":"+forgedToken)
	requireNoFlow(t, ctx, client, recordedFormID+":"+emptyToken)
	require.Equal(t, 2, fake.readCount(recordedFormID), "only the two recorded submissions read the form")
	require.NotContains(t, logs.text(), sentinelSecret)
	require.NotContains(t, logs.text(), sentinelToken)
}

// TestSubmissionBeforeTheBindingRunsIsAnswered503AndTheRetryStartsTheFlowWithRealDex covers README step 7:
// while the Worker's binding is not running, the endpoint answers 503, so Typeform retries, and the retry
// that arrives once it runs starts the Flow.
func TestSubmissionBeforeTheBindingRunsIsAnswered503AndTheRetryStartsTheFlowWithRealDex(t *testing.T) {
	fake := newFakeTypeform(t)
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, fake.connectionOption(), typeform.WithLogger(logs.logger()))
	flow := responserecorder.NewFlow(connection)
	client := startWorkerAndClient(t, flow)
	endpoint := newSubmissionEndpoint(t, connection, newSubmissionTarget(client, flow, logs.logger()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	token := uniqueToken("early")
	eventID := recordedFormID + ":" + token
	body := submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, token, true)

	require.Equal(t, http.StatusServiceUnavailable, endpoint.deliverWebhook(t, body, sentinelSecret, false),
		"no binding runs yet, so Typeform must retry")
	requireNoFlow(t, ctx, client, eventID)

	endpoint.start(t)
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, body, sentinelSecret, false), "Typeform's retry")
	require.Equal(t, typeform.GetFormBranchFound, waitForRecorded(t, ctx, client, eventID).Branch)
	require.Equal(t, 1, fake.readCount(recordedFormID))
}

func dexAddress() string {
	return environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
}

// uniqueToken is a response token unique to this run, so repeated runs never collide on one Flow ID.
func uniqueToken(prefix string) string {
	return prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func questionRefs(recorded responserecorder.RecordedQuestions) []string {
	refs := make([]string, 0, len(recorded.Questions))
	for _, question := range recorded.Questions {
		refs = append(refs, question.Field.Ref)
	}
	return refs
}

func flowIDFor(eventID string) string {
	return responserecorder.ResolveFlowID(sdkgo.TriggerEvent[typeform.FormResponseEvent]{ID: eventID})
}

// waitForRecorded waits for the submission's Flow to complete and returns its recorded questions.
func waitForRecorded(t *testing.T, ctx context.Context, client *dex.Client, eventID string) responserecorder.RecordedQuestions {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		result, err := client.WaitForFlow(waitCtx, flowIDFor(eventID), dex.WaitForFlowOptions{NeedsResults: true})
		if err == nil {
			require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
			var recorded responserecorder.RecordedQuestions
			require.NoError(t, result.DecodeSingleOutput(&recorded))
			return recorded
		}
		var notFound *dex.FlowNotFoundError
		require.True(t, errors.As(err, &notFound) && waitCtx.Err() == nil, "the submission's Flow must complete: %v", err)
		// Delivery to Dex is asynchronous after the 200, so the Flow may not exist yet.
		time.Sleep(50 * time.Millisecond)
	}
}

func requireNoFlow(t *testing.T, ctx context.Context, client *dex.Client, eventID string) {
	t.Helper()
	_, err := client.WaitForFlow(ctx, flowIDFor(eventID), dex.WaitForFlowOptions{})
	var notFound *dex.FlowNotFoundError
	require.ErrorAs(t, err, &notFound, "event %s must not start a Flow", eventID)
}

// newInspectionClient waits for and inspects Flows; it registers no Worker.
func newInspectionClient(t *testing.T) *dex.Client {
	t.Helper()
	inspectionClient, err := typeform.New(typeform.Config{}, sdkgo.StaticCredentialProvider[typeform.Credentials]{})
	require.NoError(t, err)
	connection, err := typeform.NewConnection(inspectionClient, sdkgo.ConnectionRef{Provider: "typeform", Name: responserecorder.ConnectionName})
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{responserecorder.NewFlow(connection)})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "inspection-blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexAddress()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	return client
}

func startWorkerAndClient(t *testing.T, flow *responserecorder.Flow) *dex.Client {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := "127.0.0.1:" + unusedPort(t)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: dexAddress(), WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: dexAddress(), WorkerTarget: worker.WorkerTarget()})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		require.NoError(t, errors.Join(stopWorker(worker), client.Close(), cache.Close()))
		<-workerResult
	})
	return client
}
