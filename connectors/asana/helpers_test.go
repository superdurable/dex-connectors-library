// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAccessToken = "asana-access-token"
	testTaskID      = "1204567890123456"
	testProjectID   = "1201000000000001"
	testSectionID   = "1201000000000101"
	testUserID      = "1200000000000042"
)

var asanaConnection = sdkgo.ConnectionRef{Provider: "asana", Name: "asana-test"}

type recordedRequest struct {
	method        string
	path          string
	rawQuery      string
	authorization string
	body          string
}

// recordingAsana is a credential-safe Asana fake whose handler sees each request and its index.
type recordingAsana struct {
	*httptest.Server
	t        *testing.T
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingAsana(t *testing.T, handler func(http.ResponseWriter, *http.Request, int)) *recordingAsana {
	t.Helper()
	provider := &recordingAsana{t: t}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contents, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		request.Body = io.NopCloser(bytes.NewReader(contents))
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recordedRequest{
			method: request.Method, path: request.URL.Path, rawQuery: request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"), body: string(contents),
		})
		provider.mutex.Unlock()
		handler(response, request, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingAsana) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingAsana) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	require.Greater(provider.t, len(provider.requests), index, "request %d was not sent", index)
	return provider.requests[index]
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(t, err)
}

// taskJSON renders one task resource the way Asana returns it with the connector's opt_fields.
func taskJSON(id string, name string, isCompleted bool, sectionID string) string {
	completedAt := "null"
	if isCompleted {
		completedAt = `"2026-09-30T18:00:00.000Z"`
	}
	return fmt.Sprintf(`{"gid":%q,"resource_type":"task","name":%q,"resource_subtype":"default_task","completed":%t,"completed_at":%s,`+
		`"assignee":{"gid":%q,"resource_type":"user","name":"Ada Lovelace"},"due_on":"2026-10-15","due_at":null,"start_on":null,`+
		`"memberships":[{"project":{"gid":%q,"name":"Facilities"},"section":{"gid":%q,"name":"Approved"}}],"parent":null,`+
		`"workspace":{"gid":"1100000000000001","name":"Operations"},"notes":"Badge reader at door 4 is offline.",`+
		`"custom_fields":[{"gid":"1300000000000001","name":"Request ID","resource_subtype":"text","display_value":"REQ-1042","text_value":"REQ-1042"},`+
		`{"gid":"1300000000000002","name":"Priority","resource_subtype":"enum","display_value":"High","enum_value":{"gid":"1300000000000021","name":"High"}}],`+
		`"permalink_url":"https://app.asana.com/0/%s/%s","created_at":"2026-09-30T16:15:00.000Z","modified_at":"2026-09-30T17:20:00.000Z"}`,
		id, name, isCompleted, completedAt, testUserID, testProjectID, sectionID, testProjectID, id)
}

func newAsanaClient(t *testing.T, endpoint string, options ...asana.Option) *asana.Client {
	t.Helper()
	client, err := asana.New(asana.Config{Endpoint: endpoint}, staticAsanaCredentials(), options...)
	require.NoError(t, err)
	return client
}

func staticAsanaCredentials() sdkgo.StaticCredentialProvider[asana.Credentials] {
	return sdkgo.StaticCredentialProvider[asana.Credentials]{asanaConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)}}
}

// requireNoSentinel proves a Result carries neither provider message text nor the access token.
func requireNoSentinel(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), testAccessToken)
}

func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind)
	require.NotContains(t, retry.Failure.Message, "SENTINEL")
	return retry
}

// asanaDexContext is the minimal Dex Step context the SDK needs to derive a Call.
type asanaDexContext struct {
	context.Context
	step string
}

func newAsanaDexContext(step string) *asanaDexContext {
	return &asanaDexContext{Context: context.Background(), step: step}
}

func (*asanaDexContext) FlowID() string                                  { return "asana-flow" }
func (*asanaDexContext) RunID() string                                   { return "run" }
func (*asanaDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *asanaDexContext) StepExecutionID() string                 { return context.step }
func (*asanaDexContext) FromStepExecutionID() string                     { return "" }
func (*asanaDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*asanaDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*asanaDexContext) Attempt() int32                                  { return 1 }
func (*asanaDexContext) HasTimerFired() bool                             { return false }
func (*asanaDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*asanaDexContext) WaitForMethodFailed() bool                       { return false }
func (*asanaDexContext) RecordHeartbeat(any) error                       { return nil }
func (*asanaDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*asanaDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*asanaDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*asanaDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*asanaDexContext)(nil)
