// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	issueevents "github.com/superdurable/dex-connectors-library/connectors/linear/examples/issue-events/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// sentinelAPIKey stands in for the personal API key; no log record or response may contain it.
	sentinelAPIKey = "SENTINEL-LINEAR-API-KEY"
	// sentinelSigningSecret stands in for the webhook signing secret.
	sentinelSigningSecret = "SENTINEL-LINEAR-SIGNING-SECRET"

	exampleTeamID  = "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
	otherTeamID    = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
	exampleStateID = "22222222-2222-4222-8222-222222222222"
)

// teamBinding is the teamPicker binding Dex Web saves: only issues of the example team start a Flow.
var teamBinding = linear.IssueEventReceivedTriggerConfiguration{TeamID: exampleTeamID}

// newExampleConnection builds the API-key connection Dex Web saves; a static credential replaces project storage.
func newExampleConnection(t *testing.T, options ...linear.Option) linear.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "linear", Name: issueevents.ConnectionName}
	client, err := linear.New(linear.Config{}, sdkgo.StaticCredentialProvider[linear.Credentials]{reference: {
		AuthMethodID: linear.PersonalAPIKeyAuthMethodID, APIKey: sdkgo.NewSecretString(sentinelAPIKey),
		WebhookSigningSecret: sdkgo.NewSecretString(sentinelSigningSecret),
	}}, options...)
	require.NoError(t, err)
	connection, err := linear.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// issueEndpoint serves the example's target like newIssueEndpointRunner, without the durable project inbox.
type issueEndpoint struct {
	server         *httptest.Server
	endpointRunner *webhooktrigger.EndpointRunner
	readiness      interface{ RunningSourceCount() int }
}

func newIssueEndpoint(t *testing.T, connection linear.Connection, target sdkgo.TriggerTarget[linear.IssueEvent]) *issueEndpoint {
	t.Helper()
	handler, err := connection.IssueEventReceivedWebhookHandler()
	require.NoError(t, err)
	trigger := linear.NewIssueEventReceivedTrigger(linear.IssueEventReceivedTriggerConfig{
		Connection: connection, ConnectionName: issueevents.ConnectionName,
		BindingName: issueevents.IssueCreatedTriggerBinding, Configuration: teamBinding, Target: target,
	})
	endpointRunner, err := webhooktrigger.NewEndpointRunner(handler, trigger)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(webhookPath, endpointRunner)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &issueEndpoint{server: server, endpointRunner: endpointRunner, readiness: handler.(interface{ RunningSourceCount() int })}
}

// start runs the binding until the test ends and waits until it receives deliveries.
func (endpoint *issueEndpoint) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- endpoint.endpointRunner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	require.Eventually(t, func() bool { return endpoint.readiness.RunningSourceCount() == 1 }, 10*time.Second, 10*time.Millisecond,
		"the Linear binding must start receiving")
}

// deliverWebhook posts body as Linear does, signed with signingSecret; tamper changes it after signing.
func (endpoint *issueEndpoint) deliverWebhook(t *testing.T, body string, signingSecret string, isTampered bool) int {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(body))
	if isTampered {
		body = strings.Replace(body, "Fire panel wiring", "Forged title", 1)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint.server.URL+webhookPath, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Linear-Signature", hex.EncodeToString(mac.Sum(nil)))
	response, err := endpoint.server.Client().Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), sentinelSigningSecret)
	return response.StatusCode
}

// issueWebhookBody is a Linear Issue webhook for issueID, sent at sentAt; updatedAt fixes the event ID.
func issueWebhookBody(action string, issueID string, teamID string, sentAt time.Time) string {
	return fmt.Sprintf(`{"action":%q,"type":"Issue","createdAt":"2026-10-04T11:59:59.000Z","organizationId":"d4e5f6a7-b8c9-4d0e-9f1a-2b3c4d5e6f7a",`+
		`"webhookId":"e5f6a7b8-c9d0-4e1f-8a2b-3c4d5e6f7a8b","webhookTimestamp":%d,"url":"https://linear.app/acme/issue/ENG-7",`+
		`"actor":{"id":"7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f","name":"Alice Nguyen","type":"user"},`+
		`"data":{"id":%q,"identifier":"ENG-7","number":7,"title":"Fire panel wiring","url":"https://linear.app/acme/issue/ENG-7",`+
		`"priority":0,"labelIds":[],"teamId":%q,"team":{"id":%q,"key":"ENG","name":"Engineering"},"stateId":%q,`+
		`"state":{"id":%q,"name":"Todo","type":"unstarted","color":"#e2e2e2"},"assigneeId":null,"description":"Check every panel.",`+
		`"createdAt":"2026-10-04T11:59:58.000Z","updatedAt":"2026-10-04T11:59:58.000Z","archivedAt":null,"trashed":null}}`,
		action, sentAt.UnixMilli(), issueID, teamID, teamID, exampleStateID, exampleStateID)
}

// triggerEventID is the Trigger event ID of issueWebhookBody's issue.
func triggerEventID(action string, issueID string) string {
	return action + ":" + issueID + ":" + strconv.FormatInt(time.Date(2026, 10, 4, 11, 59, 58, 0, time.UTC).UnixMilli(), 10)
}

// fakeLinear answers getIssue for every issue it knows and counts those reads.
type fakeLinear struct {
	*httptest.Server
	mu    sync.Mutex
	reads map[string]int
}

func newFakeLinear(t *testing.T) *fakeLinear {
	t.Helper()
	fake := &fakeLinear{reads: map[string]int{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			OperationName string `json:"operationName"`
			Variables     struct {
				Filter struct {
					ID struct {
						Eq string `json:"eq"`
					} `json:"id"`
				} `json:"filter"`
			} `json:"variables"`
		}
		if request.Header.Get("Authorization") != sentinelAPIKey || json.NewDecoder(request.Body).Decode(&document) != nil ||
			document.OperationName != "LinearGetIssue" {
			http.Error(response, `{"errors":[{"message":"x","extensions":{"code":"AUTHENTICATION_ERROR"}}]}`, http.StatusUnauthorized)
			return
		}
		issueID := document.Variables.Filter.ID.Eq
		fake.mu.Lock()
		fake.reads[issueID]++
		fake.mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"data":{"issues":{"nodes":[`+issueJSON(issueID)+`]}}}`) // A failed write fails the Flow.
	}))
	t.Cleanup(fake.Close)
	return fake
}

func issueJSON(issueID string) string {
	return fmt.Sprintf(`{"id":%q,"identifier":"ENG-7","number":7,"title":"Fire panel wiring","url":"https://linear.app/acme/issue/ENG-7",`+
		`"priority":2,"priorityLabel":"High","estimate":null,"dueDate":null,"labelIds":[],"branchName":"eng-7","trashed":false,`+
		`"description":"Check every panel.","createdAt":"2026-10-04T11:59:58.000Z","updatedAt":"2026-10-04T12:00:30.000Z","archivedAt":null,`+
		`"startedAt":null,"completedAt":null,"canceledAt":null,"team":{"id":%q,"key":"ENG","name":"Engineering"},`+
		`"state":{"id":%q,"name":"Todo","type":"unstarted"},"assignee":null,"creator":{"id":"7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f","name":"Alice Nguyen","displayName":"alice"},`+
		`"labels":{"nodes":[]},"project":null,"cycle":null,"parent":null}`, issueID, exampleTeamID, exampleStateID)
}

func (fake *fakeLinear) readCount(issueID string) int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.reads[issueID]
}

// connectionOption sends the connector's GraphQL requests to the fake server.
func (fake *fakeLinear) connectionOption() linear.Option {
	return linear.WithAPIURL(fake.URL + "/graphql")
}

func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

// recordedLogs keeps every record, at every level, as a message plus flattened attributes.
type recordedLogs struct {
	mu      sync.Mutex
	records []recordedLog
	output  strings.Builder
}

type recordedLog struct {
	message string
	attrs   map[string]string
}

func newRecordedLogs(t *testing.T) *recordedLogs {
	t.Helper()
	logs := &recordedLogs{}
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured logs:\n%s", logs.text())
		}
	})
	return logs
}

func (logs *recordedLogs) logger() *slog.Logger {
	return slog.New(recordedLogHandler{logs: logs})
}

// find returns the records with message whose attributes include every entry of attrs.
func (logs *recordedLogs) find(message string, attrs map[string]string) []recordedLog {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	matches := []recordedLog{}
	for _, record := range logs.records {
		if record.message != message {
			continue
		}
		isMatch := true
		for key, value := range attrs {
			isMatch = isMatch && record.attrs[key] == value
		}
		if isMatch {
			matches = append(matches, record)
		}
	}
	return matches
}

func (logs *recordedLogs) text() string {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return logs.output.String()
}

type recordedLogHandler struct {
	logs  *recordedLogs
	attrs []slog.Attr
}

func (recordedLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (handler recordedLogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := map[string]string{}
	for _, attr := range handler.attrs {
		attrs[attr.Key] = attr.Value.String()
	}
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.String()
		return true
	})
	handler.logs.mu.Lock()
	defer handler.logs.mu.Unlock()
	handler.logs.records = append(handler.logs.records, recordedLog{message: record.Message, attrs: attrs})
	fmt.Fprintf(&handler.logs.output, "level=%s msg=%q attrs=%v\n", record.Level, record.Message, attrs)
	return nil
}

func (handler recordedLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return recordedLogHandler{logs: handler.logs, attrs: append(append([]slog.Attr(nil), handler.attrs...), attrs...)}
}

func (handler recordedLogHandler) WithGroup(string) slog.Handler { return handler }
