// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const acceptedCommentBody = `{"data":{"gid":"1205000000000001","created_at":"2026-09-30T16:20:00Z"}}`

type unkeyedWriteTestOperation struct {
	name            string
	operationID     string
	objectID        string
	readPath        string
	acceptedBody    string
	successBranch   sdkgo.BranchID
	uncertainBranch sdkgo.BranchID
	run             func(*asana.Client, dex.Context) (sdkgo.BranchID, sdkgo.Receipt, error)
}

var unkeyedWriteTestOperations = []unkeyedWriteTestOperation{
	{
		name: "create task", operationID: "createTask", objectID: testTaskID, readPath: "/tasks/" + testTaskID,
		acceptedBody: createdTaskBody, successBranch: asana.CreateTaskBranchCreated, uncertainBranch: asana.CreateTaskBranchUncertain,
		run: func(client *asana.Client, stepContext dex.Context) (sdkgo.BranchID, sdkgo.Receipt, error) {
			result, err := sdkgo.RunMutation(stepContext, client.CreateTask(), asanaConnection, validCreateTaskInput())
			return result.Branch, result.Receipt, err
		},
	},
	{
		name: "add comment", operationID: "addComment", objectID: "1205000000000001", readPath: "/stories/1205000000000001",
		acceptedBody: acceptedCommentBody, successBranch: asana.AddCommentBranchAdded, uncertainBranch: asana.AddCommentBranchUncertain,
		run: func(client *asana.Client, stepContext dex.Context) (sdkgo.BranchID, sdkgo.Receipt, error) {
			result, err := sdkgo.RunMutation(stepContext, client.AddComment(), asanaConnection, asana.AddCommentInput{TaskID: testTaskID, Text: "Approved."})
			return result.Branch, result.Receipt, err
		},
	},
}

type failingAttemptCredentials struct{ attempt int32 }

func (credentials failingAttemptCredentials) Resolve(call sdkgo.Call) (asana.Credentials, error) {
	if call.Context.Attempt() == credentials.attempt {
		return asana.Credentials{}, errors.New("credential store unavailable")
	}
	return staticAsanaCredentials().Resolve(call)
}

type failingFirstHeartbeatContext struct {
	*replayAsanaDexContext
	recordCount atomic.Int32
}

type readBackResponseHandler struct {
	t         *testing.T
	operation unkeyedWriteTestOperation
	status    int
}

func (handler readBackResponseHandler) serve(response http.ResponseWriter, request *http.Request, index int) {
	if index == 0 {
		writeJSON(handler.t, response, http.StatusCreated, handler.operation.acceptedBody)
		return
	}
	require.Equal(handler.t, http.MethodGet, request.Method)
	require.Equal(handler.t, handler.operation.readPath, request.URL.Path)
	if index == 1 {
		writeJSON(handler.t, response, handler.status, `{}`)
		return
	}
	writeJSON(handler.t, response, http.StatusOK, handler.operation.acceptedBody)
}

func (context *failingFirstHeartbeatContext) RecordHeartbeat(value any) error {
	if context.recordCount.Add(1) == 1 {
		return errors.New("heartbeat store unavailable")
	}
	return context.replayAsanaDexContext.RecordHeartbeat(value)
}

func newClientWithCredentialFailure(t *testing.T, endpoint string, failedAttempt int32) *asana.Client {
	t.Helper()
	client, err := asana.New(asana.Config{Endpoint: endpoint}, failingAttemptCredentials{attempt: failedAttempt})
	require.NoError(t, err)
	return client
}

func changedCheckpoint(t *testing.T, original json.RawMessage, change func(map[string]any)) json.RawMessage {
	t.Helper()
	var checkpoint map[string]any
	require.NoError(t, json.Unmarshal(original, &checkpoint))
	change(checkpoint)
	encoded, err := json.Marshal(checkpoint)
	require.NoError(t, err)
	return encoded
}

func TestUnkeyedWritesRetryCredentialFailureAfterAcceptedIDWithoutAnotherPost(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		t.Run(operation.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, request *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusCreated, operation.acceptedBody)
					return
				}
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, operation.readPath, request.URL.Path)
				writeJSON(t, response, http.StatusOK, operation.acceptedBody)
			})
			client := newClientWithCredentialFailure(t, provider.URL, 2)
			first := newReplayAsanaDexContext("credential-id", 1, nil)
			branch, _, err := operation.run(client, first)
			require.NoError(t, err)
			require.Equal(t, operation.successBranch, branch)
			second := newReplayAsanaDexContext("credential-id", 2, first.lastHeartbeat())
			_, _, err = operation.run(client, second)
			requireRetry(t, err, sdkgo.FailureAuthentication)
			require.Equal(t, 1, provider.requestCount())
			third := newReplayAsanaDexContext("credential-id", 3, first.lastHeartbeat())
			branch, receipt, err := operation.run(client, third)
			require.NoError(t, err)
			require.Equal(t, operation.successBranch, branch)
			require.Equal(t, operation.objectID, receipt.ProviderObjectID)
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestUnkeyedWritesCredentialFailureAfterDispatchCannotSelectDefect(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		t.Run(operation.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusCreated, operation.acceptedBody)
			})
			client := newClientWithCredentialFailure(t, provider.URL, 2)
			first := newReplayAsanaDexContext("credential-dispatch", 1, nil)
			first.keepOnlyFirst = true
			branch, _, err := operation.run(client, first)
			require.NoError(t, err)
			require.Equal(t, operation.successBranch, branch)
			second := newReplayAsanaDexContext("credential-dispatch", 2, first.lastHeartbeat())
			branch, _, err = operation.run(client, second)
			require.NoError(t, err)
			require.Equal(t, operation.uncertainBranch, branch)
			missing := newReplayAsanaDexContext("credential-dispatch", 2, nil)
			branch, _, err = operation.run(client, missing)
			require.NoError(t, err)
			require.Equal(t, operation.uncertainBranch, branch)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestUnkeyedWriteReadBackOutcomesNeverResendPost(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		for _, read := range []struct {
			name    string
			status  int
			isRetry bool
		}{
			{name: "not found", status: http.StatusNotFound},
			{name: "forbidden", status: http.StatusForbidden},
			{name: "server error", status: http.StatusInternalServerError, isRetry: true},
			{name: "unavailable", status: http.StatusServiceUnavailable, isRetry: true},
		} {
			t.Run(operation.name+"/"+read.name, func(t *testing.T) {
				handler := readBackResponseHandler{t: t, operation: operation, status: read.status}
				provider := newRecordingAsana(t, handler.serve)
				client := newAsanaClient(t, provider.URL)
				first := newReplayAsanaDexContext("read-back", 1, nil)
				branch, _, err := operation.run(client, first)
				require.NoError(t, err)
				require.Equal(t, operation.successBranch, branch)
				second := newReplayAsanaDexContext("read-back", 2, first.lastHeartbeat())
				branch, receipt, err := operation.run(client, second)
				if read.isRetry {
					requireRetry(t, err, sdkgo.FailureAvailability)
					third := newReplayAsanaDexContext("read-back", 3, second.lastHeartbeat())
					branch, receipt, err = operation.run(client, third)
					require.NoError(t, err)
					require.Equal(t, operation.successBranch, branch)
					require.Equal(t, 3, provider.requestCount())
				} else {
					require.NoError(t, err)
					require.Equal(t, operation.uncertainBranch, branch)
					require.Equal(t, 2, provider.requestCount())
				}
				require.Equal(t, operation.objectID, receipt.ProviderObjectID)
			})
		}
	}
}

func TestUnkeyedWriteMismatchedReadBackIsUncertain(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		t.Run(operation.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, request *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusCreated, operation.acceptedBody)
					return
				}
				require.Equal(t, http.MethodGet, request.Method)
				writeJSON(t, response, http.StatusOK, `{"data":{"gid":"1209999999999999"}}`)
			})
			client := newAsanaClient(t, provider.URL)
			first := newReplayAsanaDexContext("mismatched-id", 1, nil)
			_, _, err := operation.run(client, first)
			require.NoError(t, err)
			second := newReplayAsanaDexContext("mismatched-id", 2, first.lastHeartbeat())
			branch, receipt, err := operation.run(client, second)
			require.NoError(t, err)
			require.Equal(t, operation.uncertainBranch, branch)
			require.Equal(t, operation.objectID, receipt.ProviderObjectID)
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestUnkeyedWriteStaleResendPermissionAfterAcceptedPostFailsClosed(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		t.Run(operation.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
					return
				}
				writeJSON(t, response, http.StatusCreated, operation.acceptedBody)
			})
			client := newAsanaClient(t, provider.URL)
			first := newReplayAsanaDexContext("stale-resend", 1, nil)
			_, _, err := operation.run(client, first)
			requireRetry(t, err, sdkgo.FailureAvailability)
			second := newReplayAsanaDexContext("stale-resend", 2, first.lastHeartbeat())
			branch, _, err := operation.run(client, second)
			require.NoError(t, err)
			require.Equal(t, operation.successBranch, branch)
			third := newReplayAsanaDexContext("stale-resend", 3, first.lastHeartbeat())
			branch, _, err = operation.run(client, third)
			require.NoError(t, err)
			require.Equal(t, operation.uncertainBranch, branch)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodPost, provider.request(0).method)
			require.Equal(t, http.MethodPost, provider.request(1).method)
		})
	}
}

func TestUnkeyedWriteCheckpointFailuresFailClosed(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		t.Run(operation.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
			})
			client := newAsanaClient(t, provider.URL)
			first := newReplayAsanaDexContext("checkpoint", 1, nil)
			_, _, err := operation.run(client, first)
			requireRetry(t, err, sdkgo.FailureAvailability)
			for _, checkpoint := range []struct {
				name    string
				attempt int32
				value   json.RawMessage
			}{
				{name: "missing", attempt: 2},
				{name: "stale", attempt: 3, value: first.lastHeartbeat()},
				{name: "foreign call", attempt: 2, value: changedCheckpoint(t, first.lastHeartbeat(), func(checkpoint map[string]any) { checkpoint["callId"] = "foreign" })},
				{name: "foreign operation", attempt: 2, value: changedCheckpoint(t, first.lastHeartbeat(), func(checkpoint map[string]any) { checkpoint["operation"] = "foreign" })},
				{name: "invalid gid", attempt: 2, value: changedCheckpoint(t, first.lastHeartbeat(), func(checkpoint map[string]any) { checkpoint["objectId"] = "invalid" })},
				{name: "unreadable", attempt: 2, value: json.RawMessage(`{"callId":`)},
			} {
				t.Run(checkpoint.name, func(t *testing.T) {
					stepContext := newReplayAsanaDexContext("checkpoint", checkpoint.attempt, checkpoint.value)
					branch, _, err := operation.run(client, stepContext)
					require.NoError(t, err)
					require.Equal(t, operation.uncertainBranch, branch)
					require.Equal(t, 1, provider.requestCount())
				})
			}
		})
	}
}

func TestUnkeyedWriteReadBackRetriesWithoutTimeForARequest(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		t.Run(operation.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusCreated, operation.acceptedBody)
			})
			client := newAsanaClient(t, provider.URL)
			first := newReplayAsanaDexContext("read-deadline", 1, nil)
			_, _, err := operation.run(client, first)
			require.NoError(t, err)
			attemptContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			second := newReplayAsanaDexContext("read-deadline", 2, first.lastHeartbeat())
			second.Context = attemptContext
			_, _, err = operation.run(client, second)
			requireRetry(t, err, sdkgo.FailureTransport)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestUnkeyedWriteCredentialFailureAfterResendPermissionIsDefect(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		t.Run(operation.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
			})
			client := newClientWithCredentialFailure(t, provider.URL, 2)
			first := newReplayAsanaDexContext("credential-resend", 1, nil)
			_, _, err := operation.run(client, first)
			requireRetry(t, err, sdkgo.FailureAvailability)
			second := newReplayAsanaDexContext("credential-resend", 2, first.lastHeartbeat())
			branch, _, err := operation.run(client, second)
			require.NoError(t, err)
			switch operation.operationID {
			case "createTask":
				require.Equal(t, asana.CreateTaskBranchDefect, branch)
			case "addComment":
				require.Equal(t, asana.AddCommentBranchDefect, branch)
			}
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestUnkeyedWriteFailedBeginRecordsResendPermissionWhenPossible(t *testing.T) {
	for _, operation := range unkeyedWriteTestOperations {
		t.Run(operation.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusCreated, operation.acceptedBody)
			})
			client := newAsanaClient(t, provider.URL)
			first := &failingFirstHeartbeatContext{replayAsanaDexContext: newReplayAsanaDexContext("failed-begin", 1, nil)}
			_, _, err := operation.run(client, first)
			requireRetry(t, err, sdkgo.FailureAvailability)
			require.GreaterOrEqual(t, first.recordCount.Load(), int32(2))
			require.Zero(t, provider.requestCount())
			second := newReplayAsanaDexContext("failed-begin", 2, first.lastHeartbeat())
			branch, _, err := operation.run(client, second)
			require.NoError(t, err)
			require.Equal(t, operation.successBranch, branch)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}
