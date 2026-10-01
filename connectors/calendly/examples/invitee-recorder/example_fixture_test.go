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
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	inviteerecorder "github.com/superdurable/dex-connectors-library/connectors/calendly/examples/invitee-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

const (
	// sentinelToken stands in for the personal access token; no log record or response may contain it.
	sentinelToken = "SENTINEL-CALENDLY-ACCESS-TOKEN"
	// sentinelSigningKey stands in for the webhook signing key.
	sentinelSigningKey = "SENTINEL-CALENDLY-SIGNING-KEY"

	bookedEventTypeURI = "https://api.calendly.com/event_types/TYPE0001"
	otherEventTypeURI  = "https://api.calendly.com/event_types/TYPE0002"
)

// exampleSetup is one connection file plus the addresses that run reads from the environment.
type exampleSetup struct {
	directory      string
	configPath     string
	webhookAddress string
	logs           *recordedLogs
}

// newExampleSetup writes the token connection and eventTypePicker binding that Dex Web saves.
func newExampleSetup(t *testing.T, dexAddress string) *exampleSetup {
	t.Helper()
	directory := t.TempDir()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": calendly.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/calendly",
			"moduleVersion": "v0.1.0", "provider": "calendly", "connectionName": inviteerecorder.ConnectionName,
			"authMethodId": calendly.PersonalAccessTokenAuthMethodID, "configuration": map[string]any{},
			"credentials": map[string]any{
				"auth_method": calendly.PersonalAccessTokenAuthMethodID, "access_token": sentinelToken, "webhook_signing_key": sentinelSigningKey,
			},
		}},
		"triggerBindings": []any{map[string]any{
			"connectorId": calendly.ConnectorID, "connectionName": inviteerecorder.ConnectionName, "triggerName": "inviteeEventReceived",
			"bindingName": inviteerecorder.InviteeCreatedTriggerBinding, "configuration": map[string]any{"eventTypeUri": bookedEventTypeURI},
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
func (setup *exampleSetup) startExample(t *testing.T, connectionOptions ...calendly.Option) *runningExample {
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
	}, 20*time.Second, 25*time.Millisecond, "the Calendly binding must start receiving")
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

// deliverWebhook posts body as Calendly does, signed now with signingKey; tamper changes it after signing.
func (setup *exampleSetup) deliverWebhook(t *testing.T, body string, signingKey string, isTampered bool) int {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(timestamp + "." + body))
	if isTampered {
		body = strings.Replace(body, "ada@", "eve@", 1)
	}
	request, err := http.NewRequest(http.MethodPost, "http://"+setup.webhookAddress+webhookPath, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Calendly-Webhook-Signature", "t="+timestamp+",v1="+hex.EncodeToString(mac.Sum(nil)))
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), sentinelSigningKey)
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

// inviteeWebhookBody is a Calendly invitee webhook for scheduled event eventID and invitee inviteeID.
func inviteeWebhookBody(webhookEvent string, eventID string, inviteeID string, eventTypeURI string) string {
	status := map[string]string{calendly.WebhookEventInviteeCreated: "active", calendly.WebhookEventInviteeCanceled: "canceled"}[webhookEvent]
	return fmt.Sprintf(`{"event":%q,"created_at":"2026-09-30T11:59:00.000000Z","created_by":"https://api.calendly.com/users/USER0001","payload":{`+
		`"uri":"https://api.calendly.com/scheduled_events/%s/invitees/%s","event":"https://api.calendly.com/scheduled_events/%s",`+
		`"email":"ada@example.com","name":"Ada Lovelace","status":%q,"timezone":"Europe/London","questions_and_answers":[],`+
		`"rescheduled":false,"old_invitee":null,"new_invitee":null,"created_at":"2026-09-30T11:58:59.000000Z","updated_at":"2026-09-30T11:58:59.000000Z",`+
		`"scheduled_event":%s}}`, webhookEvent, eventID, inviteeID, eventID, status, scheduledEventJSON(eventID, eventTypeURI))
}

func scheduledEventJSON(eventID string, eventTypeURI string) string {
	return fmt.Sprintf(`{"uri":"https://api.calendly.com/scheduled_events/%s","name":"30 Minute Meeting","status":"active",`+
		`"start_time":"2026-10-02T17:00:00.000000Z","end_time":"2026-10-02T17:30:00.000000Z","event_type":%q,`+
		`"location":{"type":"zoom","status":"pushed","join_url":"https://zoom.us/j/123","data":{"password":"ZOOM-PASSWORD"}},`+
		`"invitees_counter":{"total":1,"active":1,"limit":1},"created_at":"2026-09-30T11:58:59.000000Z","updated_at":"2026-09-30T11:58:59.000000Z",`+
		`"event_memberships":[{"user":"https://api.calendly.com/users/USER0001","user_email":"host@example.com","user_name":"Hana Host"}],"event_guests":[]}`,
		eventID, eventTypeURI)
}

// fakeCalendly answers GET /scheduled_events/{id} for every event it knows and counts those reads.
type fakeCalendly struct {
	*httptest.Server
	mu    sync.Mutex
	reads map[string]int
}

func newFakeCalendly(t *testing.T) *fakeCalendly {
	t.Helper()
	fake := &fakeCalendly{reads: map[string]int{}}
	fake.Server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		eventID, isEventRead := strings.CutPrefix(request.URL.Path, "/scheduled_events/")
		if request.Method != http.MethodGet || !isEventRead || request.Header.Get("Authorization") != "Bearer "+sentinelToken {
			http.Error(response, `{"title":"Resource Not Found"}`, http.StatusNotFound)
			return
		}
		fake.mu.Lock()
		fake.reads[eventID]++
		fake.mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"resource":`+scheduledEventJSON(eventID, bookedEventTypeURI)+`}`) // A failed write fails the Flow.
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeCalendly) readCount(eventID string) int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.reads[eventID]
}

// connectionOption sends the connector's api.calendly.com requests to the fake server.
func (fake *fakeCalendly) connectionOption() calendly.Option {
	target, err := url.Parse(fake.URL)
	if err != nil {
		panic(err)
	}
	base := fake.Client().Transport
	return calendly.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
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
