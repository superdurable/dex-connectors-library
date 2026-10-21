// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
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
			require.Equal(t, status == 429, updateErr != nil)
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
			} else {
				branch := UpdateTaskBranchProviderRejected
				if status == 404 {
					branch = UpdateTaskBranchNotFound
				}
				if status == 408 || status >= 500 {
					branch = UpdateTaskBranchUncertain
				}
				require.Equal(t, branch, update.Branch)
				require.NotContains(t, update.Failure.Message, testToken)
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
		update, err := updateTask(client, "New")
		require.NoError(t, err)
		require.Equal(t, UpdateTaskBranchUncertain, update.Branch)
		encoded, err := json.Marshal(update)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), testToken)
		server.Close()
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New(testToken + " transport details")
}
func TestTransportFailureDoesNotExposeErrorsAndMutationIsUncertain(t *testing.T) {
	client := newTestClient(t, "https://api.todoist.com/api/v1", WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	_, err := readTask(client)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testToken)
	update, err := updateTask(client, "New")
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchUncertain, update.Branch)
	require.NotContains(t, update.Failure.Message, testToken)
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
	for _, token := range []string{"", "unsafe\r\nheader", "Bearer value"} {
		invalid, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString(token)}})
		require.NoError(t, err)
		result, err := readTask(invalid)
		require.NoError(t, err)
		require.Equal(t, GetTaskBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
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

func TestUnconfirmedContentSelectsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.WriteString(w, taskJSON("A concurrent edit"))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := updateTask(newTestClient(t, server.URL), "Requested title")
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchUncertain, result.Branch)
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

func TestUpdateTaskUnconfirmedDescriptionIsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, err := io.WriteString(w, taskJSON("Original title"))
		require.NoError(t, err)
	}))
	defer server.Close()
	description := "New description"
	result, err := sdkgo.RunMutation(providerContext{}, newTestClient(t, server.URL).UpdateTask(), testConnection, UpdateTaskInput{TaskID: "6XGgmFVcrG5RRjVr", Description: &description})
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchUncertain, result.Branch)
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
			require.Equal(t, status == 429, createErr != nil)
			if createErr == nil {
				expected := CreateTaskBranchProviderRejected
				if status == 200 || status == 408 || status == 409 || status >= 500 {
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
				if status == 200 {
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
