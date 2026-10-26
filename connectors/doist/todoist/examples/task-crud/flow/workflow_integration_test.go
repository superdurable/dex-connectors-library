//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskcrud

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/doist/todoist"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationToken = "todoist-crud-integration-secret"

func TestTaskCRUDWithRealDex(t *testing.T) {
	for _, scenario := range []struct {
		name, status     string
		terminal         dex.FlowStatus
		creates, deletes int
	}{
		{"happy", "deletedAndVerified", dex.FlowCompleted, 1, 1},
		{"description-only", "deletedAndVerified", dex.FlowCompleted, 1, 1},
		{"clear-description", "deletedAndVerified", dex.FlowCompleted, 1, 1},
		{"both-fields", "deletedAndVerified", dex.FlowCompleted, 1, 1},
		{"create-rate", "deletedAndVerified", dex.FlowCompleted, 2, 1},
		{"create-service-unavailable", "deletedAndVerified", dex.FlowCompleted, 2, 1},
		{"delete-retry", "deletedAndVerified", dex.FlowCompleted, 1, 2},
		{"multi-page", "deletedAndVerified", dex.FlowCompleted, 1, 1},
		{"create-uncertain", "", dex.FlowFailed, 1, 0},
		{"create-answer-incomplete", "deletedAndVerified", dex.FlowCompleted, 1, 1},
		{"invalid-input", "", dex.FlowFailed, 0, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			provider := &crudProvider{scenario: scenario.name}
			server := httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
			defer server.Close()
			flow, harness := newCRUDIntegrationHarness(t, server.URL)
			harness.startWorker(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			input := Request{Mode: "lifecycle", Content: "Original title", Description: "Original description"}
			expectedContent, expectedDescription := "Original title - updated", input.Description
			switch scenario.name {
			case "description-only":
				expectedContent, expectedDescription = input.Content, "New description"
				input.UpdateDescription = &expectedDescription
			case "clear-description":
				expectedContent, expectedDescription = input.Content, ""
				input.UpdateDescription = &expectedDescription
			case "both-fields":
				expectedDescription = "New description"
				input.UpdateContent, input.UpdateDescription = &expectedContent, &expectedDescription
			}
			if scenario.name == "invalid-input" {
				input.Content = ""
			}
			flowID := "todoist-crud-" + scenario.name + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
			result := harness.runFlow(t, ctx, flow, flowID, input)
			require.Equal(t, scenario.terminal, result.Status, result.ErrorMessage)
			if result.Status == dex.FlowCompleted {
				var outcome Outcome
				require.NoError(t, result.DecodeSingleOutput(&outcome))
				require.Equal(t, scenario.status, outcome.Status)
				require.Equal(t, "created-task", outcome.Created.ID)
				require.Equal(t, expectedContent, outcome.Updated.Content)
				require.Equal(t, input.Description, outcome.Created.Description)
				require.Equal(t, expectedDescription, outcome.Updated.Description)
				require.Equal(t, outcome.Created.ProjectID, outcome.Updated.ProjectID)
				if scenario.name == "delete-retry" {
					require.Equal(t, "alreadyAbsent", outcome.DeleteBranch)
				} else {
					require.Equal(t, "deleted", outcome.DeleteBranch)
				}
				var display map[string]any
				require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
				encoded, err := json.Marshal(display)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), integrationToken)
			}
			provider.mutex.Lock()
			defer provider.mutex.Unlock()
			require.Equal(t, scenario.creates, provider.creates)
			require.Equal(t, scenario.deletes, provider.deletes)
			if scenario.name == "create-rate" || scenario.name == "create-service-unavailable" {
				require.Len(t, provider.requestIDs, 2)
				require.Equal(t, provider.requestIDs[0], provider.requestIDs[1])
			}
			if scenario.name == "create-service-unavailable" || scenario.name == "create-answer-incomplete" {
				require.Equal(t, 1, provider.createdTasks)
			}
			if scenario.name == "create-uncertain" {
				require.Contains(t, result.ErrorMessage, "uncertain")
				require.Contains(t, result.ErrorMessage, "Todoist request did not return a complete response")
				require.Zero(t, provider.reads)
				require.Zero(t, provider.updates)
				require.Equal(t, 1, provider.createdTasks)
			}
			if scenario.name == "multi-page" {
				require.Equal(t, 2, provider.lists)
			}
		})
	}
}
func TestTaskListReadOnlyWithRealDex(t *testing.T) {
	provider := &crudProvider{content: "Existing title", exists: true}
	server := httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	defer server.Close()
	flow, harness := newCRUDIntegrationHarness(t, server.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result := harness.runFlow(t, ctx, flow, "todoist-crud-list-"+strconv.FormatInt(time.Now().UnixNano(), 10), Request{Limit: 1})
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, "listed", outcome.Status)
	require.Len(t, outcome.Page.Tasks, 1)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Equal(t, 1, provider.lists)
	require.Zero(t, provider.creates)
	require.Zero(t, provider.updates)
	require.Zero(t, provider.deletes)
}

func TestTaskUpdateRecoveryWithRealDex(t *testing.T) {
	for _, scenario := range []struct {
		name, status            string
		terminal                dex.FlowStatus
		reads, updates, deletes int
		matches                 bool
	}{
		{"read-retry", "deletedAndVerified", dex.FlowCompleted, 4, 1, 1, true},
		{"write-retry", "deletedAndVerified", dex.FlowCompleted, 3, 2, 1, true},
		{"write-applied-503", "deletedAndVerified", dex.FlowCompleted, 3, 2, 1, true},
		{"write-unapplied-503", "deletedAndVerified", dex.FlowCompleted, 3, 2, 1, true},
		{"write-lost-response", "deletedAndVerified", dex.FlowCompleted, 3, 2, 1, true},
		{"rejected", "providerRejected", dex.FlowCompleted, 1, 1, 0, false},
		{"missing", "notFound", dex.FlowCompleted, 1, 0, 0, false},
		{"invalid-response", "", dex.FlowFailed, 1, 0, 0, false},
		{"post-update-read-missing", "notFound", dex.FlowCompleted, 2, 1, 0, false},
		{"update-answer-inactive", "notFound", dex.FlowCompleted, 2, 1, 0, false},
		{"update-read-back-forbidden", "providerRejected", dex.FlowCompleted, 2, 1, 0, false},
		{"post-update-read-rejected", "providerRejected", dex.FlowCompleted, 2, 1, 0, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			provider := &crudProvider{scenario: scenario.name}
			server := httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
			defer server.Close()
			flow, harness := newCRUDIntegrationHarness(t, server.URL)
			harness.startWorker(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			flowID := "todoist-crud-recovery-" + scenario.name + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
			result := harness.runFlow(t, ctx, flow, flowID, Request{Mode: "lifecycle", Content: "Original title"})
			require.Equal(t, scenario.terminal, result.Status, result.ErrorMessage)
			if result.Status == dex.FlowCompleted {
				var outcome Outcome
				require.NoError(t, result.DecodeSingleOutput(&outcome))
				require.Equal(t, scenario.status, outcome.Status)
				require.Equal(t, scenario.matches, outcome.MatchesRequestedFields)
				if scenario.name == "post-update-read-missing" || scenario.name == "post-update-read-rejected" {
					require.Equal(t, "updated", outcome.MutationBranch)
					require.Empty(t, outcome.DeleteBranch)
				}
				if scenario.name == "update-answer-inactive" || scenario.name == "update-read-back-forbidden" {
					require.Equal(t, scenario.status, outcome.MutationBranch)
					require.Empty(t, outcome.DeleteBranch)
				}
				var display map[string]any
				require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
				encoded, err := json.Marshal(display)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), integrationToken)
			}
			provider.mutex.Lock()
			defer provider.mutex.Unlock()
			require.Equal(t, 1, provider.creates)
			require.Equal(t, scenario.reads, provider.reads)
			require.Equal(t, scenario.updates, provider.updates)
			require.Equal(t, scenario.deletes, provider.deletes)
			if scenario.name == "write-lost-response" {
				require.Equal(t, 1, provider.contentApplications)
			}
		})
	}
}

// flakyCredentials fails its first resolution as an unavailable credential store would, then returns the token.
type flakyCredentials struct{ resolutions atomic.Int32 }

func (source *flakyCredentials) Resolve(sdkgo.Call) (todoist.Credentials, error) {
	if source.resolutions.Add(1) == 1 {
		return todoist.Credentials{}, errors.New("project object read failed")
	}
	return todoist.Credentials{APIToken: sdkgo.NewSecretString(integrationToken)}, nil
}

func TestCredentialReadFailureRetriesWithRealDex(t *testing.T) {
	provider := &crudProvider{}
	server := httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	defer server.Close()
	credentials := &flakyCredentials{}
	providerClient, err := todoist.New(todoist.Config{BaseURL: server.URL + "/api/v1"}, credentials)
	require.NoError(t, err)
	connection, err := todoist.NewConnection(providerClient, sdkgo.ConnectionRef{Provider: "todoist", Name: ConnectionName})
	require.NoError(t, err)
	flow, harness := newCRUDHarnessWithConnection(t, connection)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "todoist-crud-credential-read-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	result := harness.runFlow(t, ctx, flow, flowID, Request{Mode: "lifecycle", Content: "Original title"})
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, "deletedAndVerified", outcome.Status)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Equal(t, 1, provider.creates)
	require.Equal(t, 1, provider.createdTasks)
	require.Greater(t, credentials.resolutions.Load(), int32(1))
}

type crudProvider struct {
	mutex                                   sync.Mutex
	scenario, content, description          string
	exists                                  bool
	creates, updates, deletes, reads, lists int
	createdTasks, contentApplications       int
	requestIDs                              []string
}

func (provider *crudProvider) serveHTTP(w http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if request.Header.Get("Authorization") != "Bearer "+integrationToken {
		w.WriteHeader(401)
		return
	}
	if request.URL.Path == "/api/v1/tasks" {
		if request.Method == http.MethodPost {
			provider.creates++
			provider.requestIDs = append(provider.requestIDs, request.Header.Get("X-Request-ID"))
			if provider.scenario == "create-rate" && provider.creates == 1 {
				w.WriteHeader(429)
				return
			}
			if provider.scenario == "create-service-unavailable" && provider.creates == 1 {
				w.WriteHeader(503)
				return
			}
			var input struct {
				Content     string `json:"content"`
				Description string `json:"description"`
			}
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				w.WriteHeader(400)
				return
			}
			provider.content, provider.description = input.Content, input.Description
			provider.exists = true
			provider.createdTasks++
			if provider.scenario == "create-uncertain" {
				dropProviderResponse(w)
				return
			}
			if provider.scenario == "create-answer-incomplete" {
				writeCRUDJSON(w, map[string]any{"id": "created-task"})
				return
			}
			provider.writeTask(w)
			return
		}
		provider.lists++
		if provider.scenario == "multi-page" && request.URL.Query().Get("cursor") == "" {
			writeCRUDJSON(w, map[string]any{"results": []any{}, "next_cursor": "next.page"})
			return
		}
		tasks := []any{}
		if provider.exists {
			tasks = append(tasks, provider.task())
		}
		writeCRUDJSON(w, map[string]any{"results": tasks, "next_cursor": nil})
		return
	}
	if request.URL.Path != "/api/v1/tasks/created-task" {
		w.WriteHeader(404)
		return
	}
	switch request.Method {
	case http.MethodGet:
		provider.reads++
		if provider.scenario == "read-retry" && provider.reads == 1 {
			w.WriteHeader(503)
			return
		}
		if provider.scenario == "missing" || (provider.scenario == "post-update-read-missing" || provider.scenario == "update-answer-inactive") && provider.reads == 2 {
			w.WriteHeader(404)
			return
		}
		if (provider.scenario == "post-update-read-rejected" || provider.scenario == "update-read-back-forbidden") && provider.reads == 2 {
			w.WriteHeader(403)
			return
		}
		if provider.scenario == "invalid-response" {
			writeCRUDJSON(w, map[string]any{})
			return
		}
		if !provider.exists {
			w.WriteHeader(404)
			return
		}
		provider.writeTask(w)
	case http.MethodPost:
		provider.updates++
		if provider.scenario == "write-retry" && provider.updates == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		if provider.scenario == "rejected" {
			w.WriteHeader(403)
			return
		}
		if provider.scenario == "write-unapplied-503" && provider.updates == 1 {
			w.WriteHeader(503)
			return
		}
		var input struct {
			Content     *string `json:"content"`
			Description *string `json:"description"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			w.WriteHeader(400)
			return
		}
		if input.Content != nil {
			if provider.content != *input.Content {
				provider.contentApplications++
			}
			provider.content = *input.Content
		}
		if input.Description != nil {
			provider.description = *input.Description
		}
		if provider.scenario == "update-answer-inactive" || provider.scenario == "update-read-back-forbidden" {
			writeCRUDJSON(w, map[string]any{})
			return
		}
		if provider.scenario == "write-applied-503" && provider.updates == 1 {
			w.WriteHeader(503)
			return
		}
		if provider.scenario == "write-lost-response" && provider.updates == 1 {
			dropProviderResponse(w)
			return
		}
		provider.writeTask(w)
	case http.MethodDelete:
		provider.deletes++
		if !provider.exists {
			w.WriteHeader(404)
			return
		}
		provider.exists = false
		if provider.scenario == "delete-retry" {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}
func (provider *crudProvider) task() map[string]any {
	return map[string]any{"id": "created-task", "project_id": "test-project", "content": provider.content, "description": provider.description, "checked": false}
}
func (provider *crudProvider) writeTask(w http.ResponseWriter) { writeCRUDJSON(w, provider.task()) }
func writeCRUDJSON(w http.ResponseWriter, value any) {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(err)
	}
}

func dropProviderResponse(w http.ResponseWriter) {
	connection, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		panic(err)
	}
	if err := connection.Close(); err != nil {
		panic(err)
	}
}

type crudIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newCRUDIntegrationHarness(t *testing.T, providerURL string) (*TodoistTaskCRUDFlow, *crudIntegrationHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "todoist", Name: ConnectionName}
	providerClient, err := todoist.New(todoist.Config{BaseURL: providerURL + "/api/v1"}, sdkgo.StaticCredentialProvider[todoist.Credentials]{
		reference: {APIToken: sdkgo.NewSecretString(integrationToken)},
	})
	require.NoError(t, err)
	connection, err := todoist.NewConnection(providerClient, reference)
	require.NoError(t, err)
	return newCRUDHarnessWithConnection(t, connection)
}

func newCRUDHarnessWithConnection(t *testing.T, connection todoist.Connection) (*TodoistTaskCRUDFlow, *crudIntegrationHarness) {
	t.Helper()
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &crudIntegrationHarness{
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"), workerAddress: workerAddress,
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if harness.worker != nil {
			harness.stopWorker(t)
		}
		require.NoError(t, errors.Join(harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func (harness *crudIntegrationHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *crudIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func (harness *crudIntegrationHarness) runFlow(t *testing.T, ctx context.Context, flow *TodoistTaskCRUDFlow, flowID string, input Request) dex.FlowResult {
	t.Helper()
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	return result
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
