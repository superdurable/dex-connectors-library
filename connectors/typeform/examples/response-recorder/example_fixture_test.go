// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	responserecorder "github.com/superdurable/dex-connectors-library/connectors/typeform/examples/response-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

const (
	// sentinelToken stands in for the personal access token; no log record or response may contain it.
	sentinelToken = "SENTINEL-TYPEFORM-ACCESS-TOKEN"
	// sentinelSecret stands in for the webhook secret.
	sentinelSecret = "SENTINEL-TYPEFORM-WEBHOOK-SECRET"

	recordedFormID = "lT4Z3j"
	otherFormID    = "u6nXL7"
)

// exampleSetup is one connection file plus the addresses that run reads from the environment.
type exampleSetup struct {
	directory      string
	configPath     string
	webhookAddress string
	logs           *recordedLogs
}

// newExampleSetup writes the token connection and formPicker binding that Dex Web saves.
func newExampleSetup(t *testing.T, dexAddress string) *exampleSetup {
	t.Helper()
	directory := t.TempDir()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": typeform.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/typeform",
			"moduleVersion": "v0.1.0", "provider": "typeform", "connectionName": responserecorder.ConnectionName,
			"authMethodId": typeform.PersonalAccessTokenAuthMethodID, "configuration": map[string]any{},
			"credentials": map[string]any{
				"auth_method": typeform.PersonalAccessTokenAuthMethodID, "access_token": sentinelToken, "webhook_secret": sentinelSecret,
			},
		}},
		"triggerBindings": []any{map[string]any{
			"connectorId": typeform.ConnectorID, "connectionName": responserecorder.ConnectionName, "triggerName": "responseSubmitted",
			"bindingName": responserecorder.ResponseSubmittedTriggerBinding, "configuration": map[string]any{"formId": recordedFormID},
		}},
	})
	require.NoError(t, err)
	setup := &exampleSetup{
		directory: directory, configPath: filepath.Join(directory, "connections.json"),
		webhookAddress: "127.0.0.1:" + unusedPort(t), logs: newRecordedLogs(),
	}
	require.NoError(t, os.WriteFile(setup.configPath, contents, 0o600))
	t.Setenv(localconfig.EnvironmentVariable, setup.configPath)
	t.Setenv("DEX_FLOW_SERVICE_ADDRESS", dexAddress)
	t.Setenv("WEBHOOK_BIND_ADDRESS", setup.webhookAddress)
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured logs:\n%s", setup.logs.text())
		}
	})
	return setup
}

// runningExample is one run of the example's run function.
type runningExample struct {
	cancel context.CancelFunc
	result chan error
}

// startExample calls run with a fresh Worker port and blob cache, then waits for the readiness check.
func (setup *exampleSetup) startExample(t *testing.T, connectionOptions ...typeform.Option) *runningExample {
	t.Helper()
	t.Setenv("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:"+unusedPort(t))
	t.Setenv("DEX_BLOB_CACHE_DIR", filepath.Join(setup.directory, "blobs-"+strconv.FormatInt(time.Now().UnixNano(), 10)))
	ctx, cancel := context.WithCancel(context.Background())
	running := &runningExample{cancel: cancel, result: make(chan error, 1)}
	go func() { running.result <- run(ctx, setup.logs.logger(), connectionOptions...) }()
	t.Cleanup(cancel)
	require.Eventually(t, func() bool {
		response, err := http.Get("http://" + setup.webhookAddress + readinessPath)
		if err != nil {
			return false
		}
		_ = response.Body.Close() // Only the status matters.
		return response.StatusCode == http.StatusOK
	}, 20*time.Second, 25*time.Millisecond, "the Typeform binding must start receiving")
	return running
}

func (running *runningExample) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	select {
	case err := <-running.result:
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("the example did not stop after cancellation")
	}
}

// deliverWebhook posts body as Typeform does, signed with secret; tamper changes it after signing.
func (setup *exampleSetup) deliverWebhook(t *testing.T, body string, secret string, isTampered bool) int {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	if isTampered {
		body = strings.Replace(body, "ada@", "eve@", 1)
	}
	request, err := http.NewRequest(http.MethodPost, "http://"+setup.webhookAddress+webhookPath, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Typeform-Signature", "sha256="+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), sentinelSecret)
	return response.StatusCode
}

func (setup *exampleSetup) pendingEventIDs(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(setup.directory, ".trigger-inbox-*.json"))
	require.NoError(t, err)
	eventIDs := []string{}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		var inbox struct {
			Events []struct {
				EventID string `json:"eventId"`
			} `json:"events"`
		}
		require.NoError(t, json.Unmarshal(contents, &inbox))
		for _, event := range inbox.Events {
			eventIDs = append(eventIDs, event.EventID)
		}
	}
	return eventIDs
}

// submissionBody is a Typeform form_response webhook for formID and token; withAnswers false sends none.
func submissionBody(eventType string, formID string, token string, withAnswers bool) string {
	answers := `[{"type":"text","text":"Ada","field":{"id":"JwWggjAKtOkA","type":"short_text"}},` +
		`{"type":"email","email":"ada@example.com","field":{"id":"SMEUb7VJz92Q","type":"email","ref":"email"}},` +
		`{"type":"choice","choice":{"id":"4WIlUvKOl0UB","label":"London","ref":"london"},"field":{"id":"k6TP9oLGgHjl","type":"multiple_choice","ref":"city"}}]`
	if !withAnswers {
		answers = `[]`
	}
	return fmt.Sprintf(`{"event_id":"LtWXD3crgy","event_type":%q,"form_response":{"form_id":%q,"token":%q,`+
		`"landed_at":"2026-09-30T11:50:00Z","submitted_at":"2026-09-30T11:58:59Z","hidden":{"user_id":"abc123456"},`+
		`"definition":{"id":%q,"title":"Lead intake","fields":[{"id":"JwWggjAKtOkA","ref":"first_name","title":"What is your first name?","type":"short_text"},`+
		`{"id":"SMEUb7VJz92Q","ref":"email","title":"Your email?","type":"email"},{"id":"k6TP9oLGgHjl","ref":"city","title":"Favorite city?","type":"multiple_choice"}]},`+
		`"answers":%s}}`, eventType, formID, token, formID, answers)
}

// formDefinitionJSON is GET /forms/{id} for the submitted form: the three answered questions and a
// skipped yes/no question.
func formDefinitionJSON(formID string) string {
	return fmt.Sprintf(`{"id":%q,"title":"Lead intake","hidden":["user_id"],"fields":[`+
		`{"id":"JwWggjAKtOkA","ref":"first_name","title":"What is your first name?","type":"short_text","validations":{"required":true}},`+
		`{"id":"SMEUb7VJz92Q","ref":"email","title":"Your email?","type":"email","validations":{"required":true}},`+
		`{"id":"k6TP9oLGgHjl","ref":"city","title":"Favorite city?","type":"multiple_choice","properties":{"choices":[{"id":"4WIlUvKOl0UB","ref":"london","label":"London"}]}},`+
		`{"id":"RUqkXSeXBXSd","ref":"consent","title":"May we follow up?","type":"yes_no"}]}`, formID)
}

// fakeTypeform answers GET /forms/{id} for every form and counts those reads.
type fakeTypeform struct {
	*httptest.Server
	mu    sync.Mutex
	reads map[string]int
}

func newFakeTypeform(t *testing.T) *fakeTypeform {
	t.Helper()
	fake := &fakeTypeform{reads: map[string]int{}}
	fake.Server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		formID, isFormRead := strings.CutPrefix(request.URL.Path, "/forms/")
		if request.Method != http.MethodGet || !isFormRead || request.Header.Get("Authorization") != "Bearer "+sentinelToken {
			http.Error(response, `{"code":"NOT_EXISTING_ID"}`, http.StatusNotFound)
			return
		}
		fake.mu.Lock()
		fake.reads[formID]++
		fake.mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, formDefinitionJSON(formID)) // A failed write fails the Flow.
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeTypeform) readCount(formID string) int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.reads[formID]
}

// connectionOption sends the connector's api.typeform.com requests to the fake server.
func (fake *fakeTypeform) connectionOption() typeform.Option {
	target, err := url.Parse(fake.URL)
	if err != nil {
		panic(err)
	}
	base := fake.Client().Transport
	return typeform.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(redirected)
	})})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
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

func newRecordedLogs() *recordedLogs { return &recordedLogs{} }

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
