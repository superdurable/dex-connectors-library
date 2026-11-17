// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const createdTaskName = "Escalation: Fix login [86b2x4k00]"

func validCreateTaskInput() clickup.CreateTaskInput {
	dueAt := time.UnixMilli(1767484800000)
	return clickup.CreateTaskInput{
		ListID: testListID, Name: createdTaskName, MarkdownDescription: "Blocked task: **Fix login**", Status: "to do",
		Priority: clickup.TaskPriorityUrgent, DueAt: &dueAt, AssigneeIDs: []int64{183}, Tags: []string{"escalation"},
		CustomFields: []clickup.CustomFieldInput{{ID: "d2ab17e0-41d3-4d28-bdbb-f3f0f2c62f8b", Value: json.RawMessage(`"f8249998-9c0a-4b1d-a8b7-67a072aca05a"`)}},
	}
}

func TestCreateTaskSendsTheTaskAndRecordsItsID(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: "86b2x9z11", name: createdTaskName, assigneeIDs: []int64{183}, tags: []string{"escalation"}}))
	})
	dexContext := newTestDexContext("create")
	result, err := sdkgo.RunMutation(dexContext, newClickUpClient(t, provider.URL).CreateTask(), clickupConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, clickup.CreateTaskBranchCreated, result.Branch)
	require.Equal(t, "86b2x9z11", result.Value.Task.ID)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "86b2x9z11", result.Receipt.ProviderObjectID)
	require.Equal(t, "86b2x9z11", lastFenceMarker(t, dexContext).ProviderID, "a replayed attempt reads the accepted task back")
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/list/"+testListID+"/task", request.path)
	require.JSONEq(t, `{"name":"`+createdTaskName+`","markdown_content":"Blocked task: **Fix login**","status":"to do","priority":1,
		"due_date":1767484800000,"due_date_time":true,"assignees":[183],"tags":["escalation"],
		"custom_fields":[{"id":"d2ab17e0-41d3-4d28-bdbb-f3f0f2c62f8b","value":"f8249998-9c0a-4b1d-a8b7-67a072aca05a"}]}`, request.body)
}

func TestUnknownCreateReadsTheListBackBeforeResending(t *testing.T) {
	firstAttemptAt := time.UnixMilli(1767225600000)
	for _, test := range []struct {
		name             string
		tasks            []testTask
		expectedBranch   sdkgo.BranchID
		expectedTaskID   string
		isResent         bool
		isAlreadyApplied bool
	}{
		{name: "one matching task", tasks: []testTask{
			{id: "86b2x9z11", name: createdTaskName, createdAt: firstAttemptAt.Add(time.Second)},
			{id: "86b2x9z12", name: "Another task", createdAt: firstAttemptAt.Add(time.Second)},
		}, expectedBranch: clickup.CreateTaskBranchCreated, expectedTaskID: "86b2x9z11", isAlreadyApplied: true},
		{name: "no matching task", tasks: []testTask{{id: "86b2x9z12", name: "Another task", createdAt: firstAttemptAt}},
			expectedBranch: clickup.CreateTaskBranchCreated, expectedTaskID: "86b2x9z20", isResent: true},
		{name: "a subtask of another parent", tasks: []testTask{{id: "86b2x9z13", name: createdTaskName, parent: "86b2x4k99", createdAt: firstAttemptAt}},
			expectedBranch: clickup.CreateTaskBranchCreated, expectedTaskID: "86b2x9z20", isResent: true},
		{name: "two matching tasks", tasks: []testTask{
			{id: "86b2x9z11", name: createdTaskName, createdAt: firstAttemptAt}, {id: "86b2x9z14", name: createdTaskName, createdAt: firstAttemptAt},
		}, expectedBranch: clickup.CreateTaskBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, index int) {
				switch {
				case index == 0:
					hijackAndClose(t, response)
				case request.Method == http.MethodPost:
					writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: "86b2x9z20", name: createdTaskName}))
				default:
					writeTaskList(t, response, test.tasks)
				}
			})
			client := newClickUpClient(t, provider.URL)
			dexContext := newTestDexContext("create-" + test.name)
			dexContext.firstAttemptAt = firstAttemptAt
			_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "a lost response retries with the claim held")
			require.Equal(t, singledispatch.StateClaimed, lastFenceMarker(t, dexContext).State)

			result, err := sdkgo.RunMutation(dexContext.nextAttempt(), client.CreateTask(), clickupConnection, validCreateTaskInput())
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.expectedTaskID, result.Value.Task.ID)
			require.Equal(t, test.isAlreadyApplied, result.Value.WasAlreadyApplied)
			expectedRequests := 2
			if test.isResent {
				expectedRequests = 3
				require.Equal(t, http.MethodPost, provider.request(2).method, "read-back found nothing, so the create is sent again within the cap")
			}
			require.Equal(t, expectedRequests, provider.requestCount())
			reconcile := provider.request(1)
			parsed, err := url.ParseRequestURI(reconcile.path)
			require.NoError(t, err)
			require.Equal(t, http.MethodGet, reconcile.method)
			require.Equal(t, "/list/"+testListID+"/task", parsed.Path)
			require.Equal(t, url.Values{
				"date_created_gt": {strconv.FormatInt(firstAttemptAt.Add(-5*time.Minute).UnixMilli(), 10)},
				"include_closed":  {"true"}, "subtasks": {"true"}, "page": {"0"},
			}, parsed.Query())
		})
	}
}

func TestUnknownCreateSelectsUncertainOnlyAtTheSendCap(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusInternalServerError, clickupError("SERVER_001"))
			return
		}
		writeTaskList(t, response, nil)
	})
	client := newClickUpClient(t, provider.URL)
	dexContext := newTestDexContext("create-at-cap")
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "an unknown outcome below the cap retries")
		require.Equal(t, int32(attempt), lastFenceMarker(t, dexContext).SendCount)
		dexContext.nextAttempt()
	}
	result, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, clickup.CreateTaskBranchUncertain, result.Branch)
	require.Contains(t, result.Failure.Message, "so it is not sent again")
	posts := 0
	for index := range provider.requestCount() {
		if provider.request(index).method == http.MethodPost {
			posts++
		}
	}
	require.Equal(t, 3, posts, "possible sends never exceed the default cap of three")
	require.Equal(t, http.MethodGet, provider.request(provider.requestCount()-1).method, "the attempt at the cap only reads back")
}

func TestAcceptedCreateIsReadBackByItsIDAndNeverResent(t *testing.T) {
	for _, test := range []struct {
		name           string
		readStatus     int
		expectedBranch sdkgo.BranchID
	}{
		{name: "task found", readStatus: http.StatusOK, expectedBranch: clickup.CreateTaskBranchCreated},
		{name: "task missing", readStatus: http.StatusNotFound, expectedBranch: clickup.CreateTaskBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				switch {
				case index == 0:
					writeJSON(t, response, http.StatusOK, `{"id":"86b2x9z11","name":7}`)
				case test.readStatus == http.StatusOK:
					writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: "86b2x9z11", name: createdTaskName}))
				default:
					writeJSON(t, response, test.readStatus, clickupError("ITEM_013"))
				}
			})
			client := newClickUpClient(t, provider.URL)
			dexContext := newTestDexContext("create-accepted-" + test.name)
			_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "an unusable 2xx retries")
			require.Equal(t, "86b2x9z11", lastFenceMarker(t, dexContext).ProviderID)

			result, err := sdkgo.RunMutation(dexContext.nextAttempt(), client.CreateTask(), clickupConnection, validCreateTaskInput())
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, 2, provider.requestCount(), "an identified task is never sent again")
			require.Equal(t, http.MethodGet, provider.request(1).method)
			require.Equal(t, "/task/86b2x9z11", provider.request(1).path)
			if test.expectedBranch == clickup.CreateTaskBranchCreated {
				require.True(t, result.Value.WasAlreadyApplied)
			}
		})
	}
}

func TestCreateTaskResendsWithoutReadBackWhenClickUpCannotHaveAppliedIt(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					response.Header().Set("X-RateLimit-Reset", "1")
					writeJSON(t, response, status, clickupError("RATE_001"))
					return
				}
				writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: "86b2x9z11", name: createdTaskName}))
			})
			dexContext := newTestDexContext("create-not-applied")
			client := newClickUpClient(t, provider.URL)
			_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, singledispatch.StateCanResend, lastFenceMarker(t, dexContext).State)
			result, err := sdkgo.RunMutation(dexContext.nextAttempt(), client.CreateTask(), clickupConnection, validCreateTaskInput())
			require.NoError(t, err)
			require.Equal(t, clickup.CreateTaskBranchCreated, result.Branch)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodPost, provider.request(1).method, "the create is sent again without a read-back")
		})
	}

	unreachable := newClickUpClient(t, "http://"+closedLoopbackAddress(t))
	notSentContext := newTestDexContext("create-unreachable")
	_, err := sdkgo.RunMutation(notSentContext, unreachable.CreateTask(), clickupConnection, validCreateTaskInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, "ClickUp could not be reached; no request was sent", retry.Failure.Message)
	require.Equal(t, singledispatch.StateCanResend, lastFenceMarker(t, notSentContext).State)
}

func TestCreateTaskSelectsARefusalOnlyForADefiniteStatus(t *testing.T) {
	for _, test := range []struct {
		status         int
		expectedBranch sdkgo.BranchID
	}{
		{status: http.StatusBadRequest, expectedBranch: clickup.CreateTaskBranchProviderRejected},
		{status: http.StatusUnprocessableEntity, expectedBranch: clickup.CreateTaskBranchProviderRejected},
		{status: http.StatusNotFound, expectedBranch: clickup.CreateTaskBranchNotFound},
		{status: http.StatusConflict},
		{status: http.StatusGone},
		{status: http.StatusTooEarly},
		{status: http.StatusPermanentRedirect},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, clickupError("ITEM_015"))
			})
			dexContext := newTestDexContext("create-status")
			result, err := sdkgo.RunMutation(dexContext, newClickUpClient(t, provider.URL).CreateTask(), clickupConnection, validCreateTaskInput())
			if test.expectedBranch == "" {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "a status that proves no refusal is an unknown outcome")
				require.Equal(t, singledispatch.StateClaimed, lastFenceMarker(t, dexContext).State)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, createdTaskName, result.Value.Name)
			require.Empty(t, result.Value.Task.ID)
			require.NotContains(t, result.Failure.Message, providerSentinel)
			require.Equal(t, singledispatch.StateCanResend, lastFenceMarker(t, dexContext).State, "a refused first send records canResend")
		})
	}
}

func TestCreateTaskRefusalAfterAPossibleSendIsUncertain(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 0:
			hijackAndClose(t, response)
		case request.Method == http.MethodGet:
			writeTaskList(t, response, nil)
		default:
			writeJSON(t, response, http.StatusBadRequest, clickupError("ITEM_015"))
		}
	})
	client := newClickUpClient(t, provider.URL)
	dexContext := newTestDexContext("create-refused-after-send")
	_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	result, err := sdkgo.RunMutation(dexContext.nextAttempt(), client.CreateTask(), clickupConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, clickup.CreateTaskBranchUncertain, result.Branch, "the first send may have created the task")
}

func TestCreateTaskCheckpointFailuresNeverSend(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: "86b2x9z11", name: createdTaskName}))
	})
	client := newClickUpClient(t, provider.URL)
	failingContext := newTestDexContext("create-no-checkpoint")
	failingContext.recordErr = errors.New("dex unavailable")
	_, err := sdkgo.RunMutation(failingContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)

	for name, heartbeat := range map[string]string{
		"an earlier connector checkpoint": `{"clickupDispatchedCallId":"another-call","clickupDispatchedAt":1767225600000}`,
		"another call's marker":           `{"callId":"another-call","attempt":1,"state":"claimed"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dexContext := newTestDexContext("create-fail-closed")
			dexContext.heartbeat, dexContext.attempt = json.RawMessage(heartbeat), 2
			_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
			require.ErrorAs(t, err, &retry, "an untrusted checkpoint retries instead of selecting a branch")
		})
	}
	require.Zero(t, provider.requestCount(), "nothing is sent without a trusted checkpoint")
}

func TestCreateTaskValidatesTheTaskWithoutARequest(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	for name, change := range map[string]func(*clickup.CreateTaskInput){
		"list":               func(input *clickup.CreateTaskInput) { input.ListID = "Escalations" },
		"blank name":         func(input *clickup.CreateTaskInput) { input.Name = "  " },
		"priority":           func(input *clickup.CreateTaskInput) { input.Priority = 5 },
		"duplicate assignee": func(input *clickup.CreateTaskInput) { input.AssigneeIDs = []int64{183, 183} },
		"custom field value": func(input *clickup.CreateTaskInput) { input.CustomFields[0].Value = json.RawMessage(`{`) },
		"parent":             func(input *clickup.CreateTaskInput) { input.ParentTaskID = "a/b" },
	} {
		t.Run(name, func(t *testing.T) {
			input := validCreateTaskInput()
			change(&input)
			result, err := sdkgo.RunMutation(newTestDexContext("create-invalid-"+name), newClickUpClient(t, provider.URL).CreateTask(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, clickup.CreateTaskBranchDefect, result.Branch)
		})
	}
	require.Zero(t, provider.requestCount())
}

// writeTaskList answers a List read with tasks.
func writeTaskList(t *testing.T, response http.ResponseWriter, tasks []testTask) {
	t.Helper()
	encodedTasks := []any{}
	for _, task := range tasks {
		encodedTasks = append(encodedTasks, taskMap(task))
	}
	encoded, err := json.Marshal(map[string]any{"tasks": encodedTasks, "last_page": true})
	require.NoError(t, err)
	writeJSON(t, response, http.StatusOK, string(encoded))
}

// lastFenceMarker decodes the single-dispatch marker that the last attempt recorded.
func lastFenceMarker(t *testing.T, dexContext *testDexContext) singledispatch.Marker {
	t.Helper()
	var marker singledispatch.Marker
	isFound, err := dexContext.GetLastHeartbeatValue(&marker)
	require.NoError(t, err)
	require.True(t, isFound)
	return marker
}

// hijackAndClose drops the connection after ClickUp received the request, so its outcome is unknown.
func hijackAndClose(t *testing.T, response http.ResponseWriter) {
	t.Helper()
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

// closedLoopbackAddress returns a loopback address with no listener, so a dial fails before sending.
func closedLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}
