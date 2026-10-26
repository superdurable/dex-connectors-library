// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testToken = "todoist-test-token-never-persist"

var testConnection = sdkgo.ConnectionRef{Provider: "todoist", Name: "todoist"}

type providerContext struct{ dex.Context }

func (providerContext) FlowID() string              { return "provider-test" }
func (providerContext) StepExecutionID() string     { return "operation-test" }
func (providerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerContext) Done() <-chan struct{}       { return nil }
func (providerContext) Err() error                  { return nil }
func (providerContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string, options ...Option) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString(testToken)}}, options...)
	require.NoError(t, err)
	return client
}
func taskJSON(content string) string {
	return fmt.Sprintf(`{"id":"6XGgmFVcrG5RRjVr","project_id":"6XGgm6PHrGgMpCFX","content":%q,"description":"Details","checked":false}`, content)
}
func readTask(client *Client) (GetTaskResult, error) {
	return sdkgo.RunQuery(providerContext{}, client.GetTask(), testConnection, GetTaskInput{TaskID: "6XGgmFVcrG5RRjVr"})
}
func updateTask(client *Client, content string) (UpdateTaskResult, error) {
	return sdkgo.RunMutation(providerContext{}, client.UpdateTask(), testConnection, UpdateTaskInput{TaskID: "6XGgmFVcrG5RRjVr", Content: &content})
}

func TestReadAndUpdateUseV1PathsAndHeaderOnlyCredentials(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/api/v1/tasks/6XGgmFVcrG5RRjVr", r.URL.Path)
		require.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.RawQuery)
		content := "Original"
		if r.Method == http.MethodPost {
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.Equal(t, map[string]any{"content": "New 标题 & text"}, payload)
			content = payload["content"].(string)
		} else {
			require.Equal(t, http.MethodGet, r.Method)
		}
		_, err := io.WriteString(w, taskJSON(content))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/v1")
	read, err := readTask(client)
	require.NoError(t, err)
	require.Equal(t, GetTaskBranchFound, read.Branch)
	require.Equal(t, "Original", read.Value.Content)
	update, err := updateTask(client, "New 标题 & text")
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchUpdated, update.Branch)
	require.Equal(t, "New 标题 & text", update.Value.Content)
	encoded, err := json.Marshal(update)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), testToken)
	require.Equal(t, int32(2), calls.Load())
	require.Equal(t, sdkgo.IdempotencyKey("call-a"), client.UpdateTask().IdempotencyKey("call-a", UpdateTaskInput{}))
	require.NotEqual(t, client.UpdateTask().IdempotencyKey("call-a", UpdateTaskInput{}), client.UpdateTask().IdempotencyKey("call-b", UpdateTaskInput{}))
}

func TestHTTPOutcomesAndRetryAfter(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 429, 408, 500, 503, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(status)
				_, err := io.WriteString(w, testToken+" private provider body")
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			read, readErr := readTask(client)
			update, updateErr := updateTask(client, "New")
			isQueryRetry := status == 429 || status == 408 || status >= 500
			require.Equal(t, isQueryRetry, readErr != nil)
			isUpdateRetry := status == 429 || status == 408 || status >= 500
			require.Equal(t, isUpdateRetry, updateErr != nil)
			if isQueryRetry {
				require.NotContains(t, readErr.Error(), testToken)
			} else {
				branch := GetTaskBranchProviderRejected
				if status == 404 {
					branch = GetTaskBranchNotFound
				}
				require.Equal(t, branch, read.Branch)
				require.NotContains(t, read.Failure.Message, testToken)
			}
			if status == 429 {
				var delay *dex.RetryAfterError
				require.ErrorAs(t, updateErr, &delay)
				require.Equal(t, 2*time.Second, delay.After)
			} else if isUpdateRetry {
				require.NotContains(t, updateErr.Error(), testToken)
			} else {
				branch := UpdateTaskBranchProviderRejected
				if status == 404 {
					branch = UpdateTaskBranchNotFound
				}
				require.Equal(t, branch, update.Branch)
				require.NotContains(t, update.Failure.Message, testToken)
			}
			if status == http.StatusConflict {
				require.Equal(t, sdkgo.FailureConflict, read.Failure.Kind)
				require.Equal(t, sdkgo.FailureConflict, update.Failure.Kind)
			}
		})
	}
}

func TestMalformedOversizedMismatchedAndSecretReflectingResponses(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{}`, taskJSON(testToken), strings.Replace(taskJSON("New"), "6XGgmFVcrG5RRjVr", "different", 1), strings.Repeat("x", 1<<20+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.WriteString(w, body)
			require.NoError(t, err)
		}))
		client := newTestClient(t, server.URL)
		read, err := readTask(client)
		require.NoError(t, err)
		require.Equal(t, GetTaskBranchInvalidResponse, read.Branch)
		_, err = updateTask(client, "New")
		require.Error(t, err)
		require.NotContains(t, err.Error(), testToken)
		server.Close()
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New(testToken + " transport details")
}
func TestTransportFailureDoesNotExposeErrorsAndUpdateRetries(t *testing.T) {
	client := newTestClient(t, "https://api.todoist.com/api/v1", WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	_, err := readTask(client)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testToken)
	_, err = updateTask(client, "New")
	require.Error(t, err)
	require.NotContains(t, err.Error(), testToken)
}

func TestInvalidInputAndCredentialsDoNotDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, id := range []string{"", "../task", "id?secret", "id/other", " id"} {
		result, err := sdkgo.RunQuery(providerContext{}, client.GetTask(), testConnection, GetTaskInput{TaskID: id})
		require.NoError(t, err)
		require.Equal(t, GetTaskBranchDefect, result.Branch)
	}
	for _, content := range []string{"", "  ", string([]byte{0xff}), strings.Repeat("x", 1<<20)} {
		result, err := updateTask(client, content)
		require.NoError(t, err)
		require.Equal(t, UpdateTaskBranchDefect, result.Branch)
	}
	for _, terminal := range []error{projectconfig.ErrObjectNotFound, sdkgo.ErrReauthorizationRequired, projectconfig.ErrConnectionNotSelected, projectconfig.ErrCredentialBrokerRejected} {
		failing, err := New(Config{BaseURL: server.URL}, failingCredentials{fmt.Errorf("resolve project credential: %w", terminal)})
		require.NoError(t, err)
		requireCredentialDefects(t, failing)
	}
	for _, token := range []string{"", "unsafe\r\nheader", "Bearer value"} {
		invalid, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString(token)}})
		require.NoError(t, err)
		requireCredentialDefects(t, invalid)
	}
	require.Zero(t, calls.Load())
}

// failingCredentials is a credential source whose every resolution fails with err.
type failingCredentials struct{ err error }

func (source failingCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{}, source.err
}

func TestCredentialReadFailureRetriesEveryOperation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	sources := map[string]CredentialSource{
		"untyped store read": failingCredentials{errors.New("project object read failed")},
		"timeout":            failingCredentials{fmt.Errorf("read credential: %w", context.DeadlineExceeded)},
		"broker unavailable": failingCredentials{fmt.Errorf("read shared credential: %w", projectconfig.ErrCredentialBrokerUnavailable)},
		// The static provider reports a missing connection with an untyped error, which cannot be told from a read failure.
		"static provider without the connection": sdkgo.StaticCredentialProvider[Credentials]{},
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			client, err := New(Config{BaseURL: server.URL}, source)
			require.NoError(t, err)
			_, createErr := createTask(client, CreateTaskInput{Content: "New"})
			_, readErr := readTask(client)
			_, updateErr := updateTask(client, "New")
			_, listErr := listTasks(client, ListTasksInput{})
			_, deleteErr := deleteTask(client)
			for _, err := range []error{createErr, readErr, updateErr, listErr, deleteErr} {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
				require.Equal(t, "connection credentials could not be read, so the request was not sent", retry.Failure.Message)
			}
		})
	}
	require.Zero(t, calls.Load())
}

// requireCredentialDefects checks that every operation selects defect for unusable local credentials.
func requireCredentialDefects(t *testing.T, client *Client) {
	t.Helper()
	read, err := readTask(client)
	require.NoError(t, err)
	require.Equal(t, GetTaskBranchDefect, read.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, read.Failure.Kind)
	update, err := updateTask(client, "New")
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchDefect, update.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, update.Failure.Kind)
	listed, err := listTasks(client, ListTasksInput{})
	require.NoError(t, err)
	require.Equal(t, ListTasksBranchDefect, listed.Branch)
	created, err := createTask(client, CreateTaskInput{Content: "New"})
	require.NoError(t, err)
	require.Equal(t, CreateTaskBranchDefect, created.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, created.Failure.Kind)
	deleted, err := deleteTask(client)
	require.NoError(t, err)
	require.Equal(t, DeleteTaskBranchDefect, deleted.Branch)
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client := newTestClient(t, redirect.URL)
	result, err := updateTask(client, "New")
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchProviderRejected, result.Branch)
	require.Zero(t, forwarded.Load())
}

func TestUnconfirmedContentRetriesAfterMismatchedReadBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.WriteString(w, taskJSON("A concurrent edit"))
		require.NoError(t, err)
	}))
	defer server.Close()
	_, err := updateTask(newTestClient(t, server.URL), "Requested title")
	require.Error(t, err)
}

func TestUnconfirmedContentUsesMatchingReadBack(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		content := "A concurrent edit"
		if request.Method == http.MethodGet {
			reads.Add(1)
			content = "Requested title"
		}
		_, err := io.WriteString(w, taskJSON(content))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := updateTask(newTestClient(t, server.URL), "Requested title")
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchUpdated, result.Branch)
	require.Equal(t, "Requested title", result.Value.Content)
	require.Equal(t, int32(1), reads.Load())
}

func TestInvalidUpdateAnswerUsesMatchingReadBack(t *testing.T) {
	for name, postBody := range map[string]string{
		"wrong task ID": strings.Replace(taskJSON("Requested title"), "6XGgmFVcrG5RRjVr", "other-task", 1),
		"malformed":     "{",
	} {
		t.Run(name, func(t *testing.T) {
			var reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				body := postBody
				if request.Method == http.MethodGet {
					reads.Add(1)
					body = taskJSON("Requested title")
				}
				_, err := io.WriteString(w, body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := updateTask(newTestClient(t, server.URL), "Requested title")
			require.NoError(t, err)
			require.Equal(t, UpdateTaskBranchUpdated, result.Branch)
			require.Equal(t, int32(1), reads.Load())
		})
	}
}

func TestCredentialReplacementAppliesOnNextCall(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString(testToken)}}
	expectedToken := testToken
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+expectedToken, r.Header.Get("Authorization"))
		_, err := io.WriteString(w, taskJSON("Original"))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, credentials)
	require.NoError(t, err)
	_, err = readTask(client)
	require.NoError(t, err)
	expectedToken = "replacement-token"
	credentials[testConnection] = Credentials{APIToken: sdkgo.NewSecretString(expectedToken)}
	_, err = readTask(client)
	require.NoError(t, err)
}

func TestUpdateTaskSendsOnlySpecifiedFields(t *testing.T) {
	content, description, emptyDescription := "New title", "New description", ""
	for _, scenario := range []struct {
		name                 string
		input                UpdateTaskInput
		payload              map[string]any
		content, description string
	}{
		{"content-only", UpdateTaskInput{Content: &content}, map[string]any{"content": content}, content, "Original description"},
		{"description-only", UpdateTaskInput{Description: &description}, map[string]any{"description": description}, "Original title", description},
		{"clear-description", UpdateTaskInput{Description: &emptyDescription}, map[string]any{"description": ""}, "Original title", ""},
		{"both-fields", UpdateTaskInput{Content: &content, Description: &description}, map[string]any{"content": content, "description": description}, content, description},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				var payload map[string]any
				require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
				require.Equal(t, scenario.payload, payload)
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": "6XGgmFVcrG5RRjVr", "project_id": "original-project", "content": scenario.content, "description": scenario.description, "checked": false}))
			}))
			defer server.Close()
			scenario.input.TaskID = "6XGgmFVcrG5RRjVr"
			result, err := sdkgo.RunMutation(providerContext{}, newTestClient(t, server.URL).UpdateTask(), testConnection, scenario.input)
			require.NoError(t, err)
			require.Equal(t, UpdateTaskBranchUpdated, result.Branch)
			require.Equal(t, scenario.content, result.Value.Content)
			require.Equal(t, scenario.description, result.Value.Description)
			require.Equal(t, "original-project", result.Value.ProjectID)
		})
	}
}

func TestUpdateTaskRejectsEmptyPatchAndInvalidDescriptionBeforeDispatch(t *testing.T) {
	invalidDescription, oversizedDescription := string([]byte{0xff}), strings.Repeat("x", 1<<20)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, input := range []UpdateTaskInput{{}, {Description: &invalidDescription}, {Description: &oversizedDescription}} {
		input.TaskID = "6XGgmFVcrG5RRjVr"
		result, err := sdkgo.RunMutation(providerContext{}, client.UpdateTask(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, UpdateTaskBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}

func TestUpdateTaskUnconfirmedDescriptionRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, err := io.WriteString(w, taskJSON("Original title"))
		require.NoError(t, err)
	}))
	defer server.Close()
	description := "New description"
	_, err := sdkgo.RunMutation(providerContext{}, newTestClient(t, server.URL).UpdateTask(), testConnection, UpdateTaskInput{TaskID: "6XGgmFVcrG5RRjVr", Description: &description})
	require.Error(t, err)
}

func listTasks(client *Client, input ListTasksInput) (ListTasksResult, error) {
	return sdkgo.RunQuery(providerContext{}, client.ListTasks(), testConnection, input)
}
func createTask(client *Client, input CreateTaskInput) (CreateTaskResult, error) {
	return sdkgo.RunMutation(providerContext{}, client.CreateTask(), testConnection, input)
}
func deleteTask(client *Client) (DeleteTaskResult, error) {
	return sdkgo.RunMutation(providerContext{}, client.DeleteTask(), testConnection, DeleteTaskInput{TaskID: "6XGgmFVcrG5RRjVr"})
}

func TestListTasksPaginationAndEmptyPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/tasks", r.URL.Path)
		require.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))
		require.Equal(t, "6XGgm6PHrGgMpCFX", r.URL.Query().Get("project_id"))
		require.Equal(t, "1", r.URL.Query().Get("limit"))
		body := `{"results":[` + taskJSON("First") + `],"next_cursor":"next.page"}`
		if r.URL.Query().Get("cursor") == "next.page" {
			body = `{"results":[],"next_cursor":null}`
		}
		_, err := io.WriteString(w, body)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := ListTasksInput{ProjectID: "6XGgm6PHrGgMpCFX", Limit: 1}
	first, err := listTasks(client, input)
	require.NoError(t, err)
	require.Equal(t, ListTasksBranchListed, first.Branch)
	require.Len(t, first.Value.Tasks, 1)
	input.Cursor = first.Value.NextCursor
	second, err := listTasks(client, input)
	require.NoError(t, err)
	require.Equal(t, ListTasksBranchListed, second.Branch)
	require.Empty(t, second.Value.Tasks)
	require.Empty(t, second.Value.NextCursor)
}

func TestCreateTaskPayloadAndStableProviderRequestIdentity(t *testing.T) {
	var requestIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/tasks", r.URL.Path)
		require.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))
		requestIDs = append(requestIDs, r.Header.Get("X-Request-ID"))
		require.Regexp(t, requestIDPattern, requestIDs[len(requestIDs)-1])
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, map[string]any{"content": "New 标题", "description": "Details", "project_id": "6XGgm6PHrGgMpCFX"}, payload)
		_, err := io.WriteString(w, taskJSON("New 标题"))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for i := 0; i < 2; i++ {
		result, err := createTask(client, CreateTaskInput{Content: "New 标题", Description: "Details", ProjectID: "6XGgm6PHrGgMpCFX"})
		require.NoError(t, err)
		require.Equal(t, CreateTaskBranchCreated, result.Branch)
	}
	require.Equal(t, requestIDs[0], requestIDs[1])
	require.NotEqual(t, client.CreateTask().IdempotencyKey("call-a", CreateTaskInput{}), client.CreateTask().IdempotencyKey("call-b", CreateTaskInput{}))
}

func TestCreateAndDeleteHTTPOutcomes(t *testing.T) {
	for _, status := range []int{200, 204, 400, 401, 403, 404, 409, 429, 408, 500, 503, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				if status != 204 {
					_, err := io.WriteString(w, testToken+" private body")
					require.NoError(t, err)
				}
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			created, createErr := createTask(client, CreateTaskInput{Content: "New"})
			require.Equal(t, status == 429 || status == 503, createErr != nil)
			if createErr == nil {
				expected := CreateTaskBranchProviderRejected
				if status == 200 || status == 204 || status == 408 || status == 409 || status == 500 {
					expected = CreateTaskBranchUncertain
				}
				require.Equal(t, expected, created.Branch)
				encoded, err := json.Marshal(created)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), testToken)
			}
			listed, listErr := listTasks(client, ListTasksInput{})
			require.Equal(t, status == 429 || status == 408 || status >= 500, listErr != nil)
			if listErr == nil {
				expected := ListTasksBranchProviderRejected
				if status == 200 || status == 204 {
					expected = ListTasksBranchInvalidResponse
				}
				require.Equal(t, expected, listed.Branch)
			}

			deleted, deleteErr := deleteTask(client)
			require.Equal(t, status == 429 || status == 408 || status >= 500, deleteErr != nil)
			if deleteErr == nil {
				expected := DeleteTaskBranchProviderRejected
				if status == 200 || status == 204 {
					expected = DeleteTaskBranchDeleted
				}
				if status == 404 {
					expected = DeleteTaskBranchAlreadyAbsent
				}
				require.Equal(t, expected, deleted.Branch)
			}
		})
	}
	client := newTestClient(t, "https://api.todoist.com/api/v1", WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	created, err := createTask(client, CreateTaskInput{Content: "New"})
	require.NoError(t, err)
	require.Equal(t, CreateTaskBranchUncertain, created.Branch)
	_, err = deleteTask(client)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testToken)
}

func TestCreateTaskConflictAfterRetryRemainsUncertain(t *testing.T) {
	var requestIDs []string
	createdTasks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		requestIDs = append(requestIDs, request.Header.Get("X-Request-ID"))
		require.Regexp(t, requestIDPattern, requestIDs[len(requestIDs)-1])
		if len(requestIDs) == 1 {
			createdTasks++
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := CreateTaskInput{Content: "New"}
	_, err := createTask(client, input)
	require.Error(t, err)
	result, err := createTask(client, input)
	require.NoError(t, err)
	require.Equal(t, CreateTaskBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, 1, createdTasks)
	require.Len(t, requestIDs, 2)
	require.Equal(t, requestIDs[0], requestIDs[1])
}

func TestCreateTaskInvalidCredentialsSelectDefect(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString("unsafe\ncredential")}})
	require.NoError(t, err)
	result, err := createTask(client, CreateTaskInput{Content: "New"})
	require.NoError(t, err)
	require.Equal(t, CreateTaskBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "unsafe")
	require.Zero(t, calls.Load())
}

func TestRefusedConnectionRetriesEveryOperation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedEndpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newTestClient(t, closedEndpoint)
	_, createErr := createTask(client, CreateTaskInput{Content: "New"})
	_, readErr := readTask(client)
	_, updateErr := updateTask(client, "New")
	_, listErr := listTasks(client, ListTasksInput{})
	_, deleteErr := deleteTask(client)
	for _, err := range []error{createErr, readErr, updateErr, listErr, deleteErr} {
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
		require.Equal(t, "Todoist could not be reached, so the request was not sent", retry.Failure.Message)
	}
}

func TestOtherSuccessStatusesAreAnswers(t *testing.T) {
	for _, scenario := range []struct {
		status        int
		body          string
		createBranch  sdkgo.BranchID
		readBranch    sdkgo.BranchID
		updateRetries bool
	}{
		{http.StatusCreated, taskJSON("New"), CreateTaskBranchCreated, GetTaskBranchFound, false},
		{http.StatusAccepted, taskJSON("New"), CreateTaskBranchCreated, GetTaskBranchFound, false},
		{http.StatusCreated, "{", CreateTaskBranchUncertain, GetTaskBranchInvalidResponse, true},
		{http.StatusNoContent, "", CreateTaskBranchUncertain, GetTaskBranchInvalidResponse, true},
	} {
		t.Run(fmt.Sprintf("%d %s", scenario.status, scenario.body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.WriteHeader(scenario.status)
				_, err := io.WriteString(w, scenario.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			created, err := createTask(client, CreateTaskInput{Content: "New"})
			require.NoError(t, err)
			require.Equal(t, scenario.createBranch, created.Branch)
			read, err := readTask(client)
			require.NoError(t, err)
			require.Equal(t, scenario.readBranch, read.Branch)
			deleted, err := deleteTask(client)
			require.NoError(t, err)
			require.Equal(t, DeleteTaskBranchDeleted, deleted.Branch)
			updated, err := updateTask(client, "New")
			if scenario.updateRetries {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, UpdateTaskBranchUpdated, updated.Branch)
		})
	}
}

func TestCreateTaskReturnsStoredTaskWhenFieldsDiffer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		_, err := io.WriteString(w, `{"id":"6XGgmFVcrG5RRjVr","project_id":"6XGgm6PHrGgMpCFX","content":"Requested title","description":"","checked":false}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := createTask(newTestClient(t, server.URL), CreateTaskInput{Content: "Requested title ", Description: "Details", ProjectID: "2203306141"})
	require.NoError(t, err)
	require.Equal(t, CreateTaskBranchCreated, result.Branch)
	require.Equal(t, Task{ID: "6XGgmFVcrG5RRjVr", ProjectID: "6XGgm6PHrGgMpCFX", Content: "Requested title"}, result.Value)
}

func TestCreateTaskUnusableAnswerRemainsUncertain(t *testing.T) {
	for name, body := range map[string]string{
		"malformed":       "{",
		"missing task ID": `{"project_id":"6XGgm6PHrGgMpCFX","content":"New","description":"","checked":false}`,
		"invalid task ID": strings.Replace(taskJSON("New"), "6XGgmFVcrG5RRjVr", "bad/id", 1),
		"reflected token": taskJSON(testToken),
	} {
		t.Run(name, func(t *testing.T) {
			var posts, reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					reads.Add(1)
				} else {
					posts.Add(1)
				}
				_, err := io.WriteString(w, body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := createTask(newTestClient(t, server.URL), CreateTaskInput{Content: "New"})
			require.NoError(t, err)
			require.Equal(t, CreateTaskBranchUncertain, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.Equal(t, Task{}, result.Value)
			require.Equal(t, int32(1), posts.Load())
			require.Zero(t, reads.Load())
		})
	}
}

func TestCreateTaskAnswerWithTaskIDIsConfirmedByRead(t *testing.T) {
	for name, postBody := range map[string]string{
		"ID only":         `{"id":"6XGgmFVcrG5RRjVr"}`,
		"missing content": `{"id":"6XGgmFVcrG5RRjVr","project_id":"6XGgm6PHrGgMpCFX","description":"","checked":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			var posts, reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					reads.Add(1)
					require.Equal(t, "/tasks/6XGgmFVcrG5RRjVr", request.URL.Path)
					require.Empty(t, request.Header.Get("X-Request-ID"))
					_, err := io.WriteString(w, taskJSON("Stored title"))
					require.NoError(t, err)
					return
				}
				posts.Add(1)
				require.Equal(t, http.MethodPost, request.Method)
				w.WriteHeader(http.StatusCreated)
				_, err := io.WriteString(w, postBody)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := createTask(newTestClient(t, server.URL), CreateTaskInput{Content: "New"})
			require.NoError(t, err)
			require.Equal(t, CreateTaskBranchCreated, result.Branch)
			require.Nil(t, result.Failure)
			require.Equal(t, Task{ID: "6XGgmFVcrG5RRjVr", ProjectID: "6XGgm6PHrGgMpCFX", Content: "Stored title", Description: "Details"}, result.Value)
			require.Equal(t, int32(1), posts.Load())
			require.Equal(t, int32(1), reads.Load())
		})
	}
}

type dropReadResponseTransport struct{}

func (dropReadResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodGet {
		return http.DefaultTransport.RoundTrip(request)
	}
	return dropResponseTransport{}.RoundTrip(request)
}

func TestCreateTaskUnconfirmedReadRemainsUncertain(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		status    int
		body      string
		transport http.RoundTripper
	}{
		{"not found", http.StatusNotFound, "", nil},
		{"forbidden", http.StatusForbidden, "", nil},
		{"rate limited", http.StatusTooManyRequests, "", nil},
		{"server error", http.StatusInternalServerError, "", nil},
		{"unavailable", http.StatusServiceUnavailable, "", nil},
		{"malformed", http.StatusOK, "{", nil},
		{"incomplete", http.StatusOK, `{"id":"6XGgmFVcrG5RRjVr"}`, nil},
		{"mismatched ID", http.StatusOK, strings.Replace(taskJSON("New"), "6XGgmFVcrG5RRjVr", "otherTask", 1), nil},
		{"lost response", http.StatusOK, taskJSON("New"), dropReadResponseTransport{}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var posts, reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					reads.Add(1)
					w.Header().Set("Retry-After", "1")
					w.WriteHeader(scenario.status)
					_, err := io.WriteString(w, scenario.body)
					require.NoError(t, err)
					return
				}
				posts.Add(1)
				_, err := io.WriteString(w, `{"id":"6XGgmFVcrG5RRjVr"}`)
				require.NoError(t, err)
			}))
			defer server.Close()
			var options []Option
			if scenario.transport != nil {
				options = append(options, WithHTTPClient(&http.Client{Transport: scenario.transport}))
			}
			result, err := createTask(newTestClient(t, server.URL, options...), CreateTaskInput{Content: "New"})
			require.NoError(t, err)
			require.Equal(t, CreateTaskBranchUncertain, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, "6XGgmFVcrG5RRjVr")
			require.Equal(t, Task{}, result.Value)
			require.Equal(t, int32(1), posts.Load())
			require.Equal(t, int32(1), reads.Load())
		})
	}
}

func TestUpdateReadBackOfInactiveTaskSelectsNotFound(t *testing.T) {
	for name, postBody := range map[string]string{
		"malformed answer":  "{",
		"mismatched answer": taskJSON("A concurrent edit"),
	} {
		t.Run(name, func(t *testing.T) {
			var posts, reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					reads.Add(1)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				posts.Add(1)
				_, err := io.WriteString(w, postBody)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := updateTask(newTestClient(t, server.URL), "Requested title")
			require.NoError(t, err)
			require.Equal(t, UpdateTaskBranchNotFound, result.Branch)
			require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, "no longer active")
			require.Equal(t, int32(1), posts.Load())
			require.Equal(t, int32(1), reads.Load())
		})
	}
}

func TestUpdateReadBackRejectionSelectsProviderRejected(t *testing.T) {
	for _, scenario := range []struct {
		status int
		kind   sdkgo.FailureKind
	}{
		{http.StatusUnauthorized, sdkgo.FailureAuthentication},
		{http.StatusForbidden, sdkgo.FailureAuthorization},
		{http.StatusBadRequest, sdkgo.FailureValidation},
		{http.StatusUnprocessableEntity, sdkgo.FailureValidation},
		{http.StatusConflict, sdkgo.FailureConflict},
		{http.StatusGone, sdkgo.FailureProviderRejection},
		{http.StatusFound, sdkgo.FailureProviderRejection},
		{http.StatusRequestTimeout, ""},
		{http.StatusTooManyRequests, ""},
		{http.StatusInternalServerError, ""},
		{http.StatusServiceUnavailable, ""},
	} {
		t.Run(fmt.Sprint(scenario.status), func(t *testing.T) {
			var posts, reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					reads.Add(1)
					w.Header().Set("Location", "/elsewhere")
					w.WriteHeader(scenario.status)
					return
				}
				posts.Add(1)
				_, err := io.WriteString(w, "{")
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := updateTask(newTestClient(t, server.URL), "Requested title")
			require.Equal(t, int32(1), posts.Load())
			require.Equal(t, int32(1), reads.Load())
			if scenario.kind == "" {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, UpdateTaskBranchProviderRejected, result.Branch)
			require.Equal(t, scenario.kind, result.Failure.Kind)
			require.Equal(t, fmt.Sprintf("Todoist rejected the read-back with HTTP %d, so the update could not be confirmed; repeating it is safe because it sets absolute field values", scenario.status), result.Failure.Message)
		})
	}
}

type dropResponseTransport struct{}

func (dropResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if err := response.Body.Close(); err != nil {
		return nil, err
	}
	return nil, errors.New("response lost")
}

func TestCreateTaskLostResponseKeepsOneUncertainTask(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Regexp(t, requestIDPattern, request.Header.Get("X-Request-ID"))
		_, err := io.WriteString(w, taskJSON("New"))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, WithHTTPClient(&http.Client{Transport: dropResponseTransport{}}))
	result, err := createTask(client, CreateTaskInput{Content: "New", Description: "Details"})
	require.NoError(t, err)
	require.Equal(t, CreateTaskBranchUncertain, result.Branch)
	require.Equal(t, int32(1), calls.Load())
}

func TestListRejectsMalformedPages(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{}`, `{"results":null,"next_cursor":null}`, `{"results":[],"next_cursor":"bad?cursor"}`, `{"results":[],"next_cursor":"same.page"}`, `{"results":[` + taskJSON("One") + `,` + taskJSON("One") + `],"next_cursor":null}`, `{"results":[` + taskJSON(testToken) + `],"next_cursor":null}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.WriteString(w, body)
			require.NoError(t, err)
		}))
		result, err := listTasks(newTestClient(t, server.URL), ListTasksInput{Cursor: "same.page"})
		require.NoError(t, err)
		require.Equal(t, ListTasksBranchInvalidResponse, result.Branch)
		server.Close()
	}
}

func TestCRUDInvalidInputDoesNotDispatch(t *testing.T) {
	client := newTestClient(t, "https://api.todoist.com/api/v1", WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	for _, input := range []ListTasksInput{{Limit: -1}, {Limit: 201}, {Cursor: "../cursor"}, {ProjectID: "a/b"}} {
		result, err := listTasks(client, input)
		require.NoError(t, err)
		require.Equal(t, ListTasksBranchDefect, result.Branch)
	}
	for _, input := range []CreateTaskInput{{}, {Content: "  "}, {Content: "New", ProjectID: "bad/id"}, {Content: strings.Repeat("a", 1<<20)}, {Content: "New", Description: string([]byte{0xff})}} {
		result, err := createTask(client, input)
		require.NoError(t, err)
		require.Equal(t, CreateTaskBranchDefect, result.Branch)
	}
	result, err := sdkgo.RunMutation(providerContext{}, client.DeleteTask(), testConnection, DeleteTaskInput{TaskID: "../task"})
	require.NoError(t, err)
	require.Equal(t, DeleteTaskBranchDefect, result.Branch)
}

// deadlineProviderContext carries a real attempt deadline, as the Dex Step context does.
type deadlineProviderContext struct {
	providerContext
	stepContext context.Context
}

func (providerContext deadlineProviderContext) Deadline() (time.Time, bool) {
	return providerContext.stepContext.Deadline()
}
func (providerContext deadlineProviderContext) Done() <-chan struct{} {
	return providerContext.stepContext.Done()
}
func (providerContext deadlineProviderContext) Err() error { return providerContext.stepContext.Err() }
func (providerContext deadlineProviderContext) Value(key any) any {
	return providerContext.stepContext.Value(key)
}

func TestCreateTaskStalledRequestsReturnBeforeTheExecuteDeadline(t *testing.T) {
	for _, scenario := range []struct {
		name                 string
		isPostStalled        bool
		callerClient         *http.Client
		wantPosts, wantReads int32
	}{
		{name: "stalled read-back", wantPosts: 1, wantReads: 1},
		{name: "stalled read-back with a long caller timeout", callerClient: &http.Client{Timeout: time.Hour}, wantPosts: 1, wantReads: 1},
		{name: "stalled creation", isPostStalled: true, wantPosts: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			var posts, reads atomic.Int32
			releaseStalledRequests := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					reads.Add(1)
					<-releaseStalledRequests
					return
				}
				posts.Add(1)
				if scenario.isPostStalled {
					<-releaseStalledRequests
					return
				}
				_, err := io.WriteString(w, `{"id":"6XGgmFVcrG5RRjVr"}`)
				require.NoError(t, err)
			}))
			defer server.Close()
			defer close(releaseStalledRequests)
			var options []Option
			if scenario.callerClient != nil {
				options = append(options, WithHTTPClient(scenario.callerClient))
			}
			stepContext, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			stepDeadline, _ := stepContext.Deadline()
			result, err := sdkgo.RunMutation(deadlineProviderContext{stepContext: stepContext}, newTestClient(t, server.URL, options...).CreateTask(), testConnection, CreateTaskInput{Content: "New"})
			require.True(t, time.Now().Before(stepDeadline.Add(-500*time.Millisecond)), "Invoke must return before the Execute deadline")
			require.NoError(t, err)
			require.Equal(t, CreateTaskBranchUncertain, result.Branch)
			require.Equal(t, scenario.wantPosts, posts.Load())
			require.Equal(t, scenario.wantReads, reads.Load())
		})
	}
}

func TestUpdateTaskStalledReadBackRetriesBeforeTheExecuteDeadline(t *testing.T) {
	t.Parallel()
	var posts, reads atomic.Int32
	releaseStalledRead := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			reads.Add(1)
			<-releaseStalledRead
			return
		}
		posts.Add(1)
		_, err := io.WriteString(w, "{")
		require.NoError(t, err)
	}))
	defer server.Close()
	defer close(releaseStalledRead)
	stepContext, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	stepDeadline, _ := stepContext.Deadline()
	content := "New"
	_, err := sdkgo.RunMutation(deadlineProviderContext{stepContext: stepContext}, newTestClient(t, server.URL).UpdateTask(), testConnection, UpdateTaskInput{TaskID: "6XGgmFVcrG5RRjVr", Content: &content})
	require.True(t, time.Now().Before(stepDeadline.Add(-500*time.Millisecond)), "Invoke must return before the Execute deadline")
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, int32(1), posts.Load())
	require.Equal(t, int32(1), reads.Load())
}

func TestExpiredAttemptRetriesWithoutSending(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, err := io.WriteString(w, taskJSON("New"))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	content := "New"
	for name, invoke := range map[string]func(dex.Context) error{
		"createTask": func(stepContext dex.Context) error {
			_, err := sdkgo.RunMutation(stepContext, client.CreateTask(), testConnection, CreateTaskInput{Content: content})
			return err
		},
		"updateTaskContent": func(stepContext dex.Context) error {
			_, err := sdkgo.RunMutation(stepContext, client.UpdateTask(), testConnection, UpdateTaskInput{TaskID: "6XGgmFVcrG5RRjVr", Content: &content})
			return err
		},
		"deleteTask": func(stepContext dex.Context) error {
			_, err := sdkgo.RunMutation(stepContext, client.DeleteTask(), testConnection, DeleteTaskInput{TaskID: "6XGgmFVcrG5RRjVr"})
			return err
		},
		"getTask": func(stepContext dex.Context) error {
			_, err := sdkgo.RunQuery(stepContext, client.GetTask(), testConnection, GetTaskInput{TaskID: "6XGgmFVcrG5RRjVr"})
			return err
		},
		"listTasks": func(stepContext dex.Context) error {
			_, err := sdkgo.RunQuery(stepContext, client.ListTasks(), testConnection, ListTasksInput{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			stepContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			err := invoke(deadlineProviderContext{stepContext: stepContext})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			require.Contains(t, retry.Failure.Message, "request was not sent")
			require.Zero(t, requests.Load())
		})
	}
}

func TestCreatedTaskReadBackWithoutTimeLeftStaysUncertain(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, err := io.WriteString(w, taskJSON("New"))
		require.NoError(t, err)
	}))
	defer server.Close()
	budget := attemptBudget{stepContext: context.Background(), attemptDeadline: time.Now().Add(time.Second), minimumRequestTime: time.Minute}
	call := sdkgo.Call{Context: providerContext{}, Connection: testConnection}
	attempt := newTestClient(t, server.URL).CreateTask().readCreatedTask(call, budget, "6XGgmFVcrG5RRjVr")
	require.Equal(t, sdkgo.NewMutationUncertain(Task{}, *failure("createTask", sdkgo.FailureProtocol, "Todoist accepted the creation of task 6XGgmFVcrG5RRjVr but it could not be read back in this attempt, so the task is unconfirmed"), sdkgo.Receipt{}), attempt)
	require.Zero(t, requests.Load())
}

func TestAttemptBudgetUsesTheShorterStepDeadline(t *testing.T) {
	stepContext, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	before := time.Now()
	budget := newAttemptBudget(stepContext, 30*time.Second)
	require.WithinDuration(t, before.Add(7*time.Second), budget.writeDeadline, 500*time.Millisecond)
	require.WithinDuration(t, before.Add(10*time.Second), budget.attemptDeadline, 500*time.Millisecond)
	require.InDelta(t, float64(1200*time.Millisecond), float64(budget.minimumRequestTime), float64(50*time.Millisecond))
	unbounded := newAttemptBudget(context.Background(), 30*time.Second)
	require.Equal(t, 3*time.Second, unbounded.minimumRequestTime)
	require.WithinDuration(t, time.Now().Add(25*time.Second), unbounded.attemptDeadline, 500*time.Millisecond)
}
