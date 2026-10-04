// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	inviteerecorder "github.com/superdurable/dex-connectors-library/connectors/calendly/examples/invitee-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// sentinelToken stands in for the personal access token; no log record or response may contain it.
	sentinelToken = "SENTINEL-CALENDLY-ACCESS-TOKEN"
	// sentinelSigningKey stands in for the webhook signing key.
	sentinelSigningKey = "SENTINEL-CALENDLY-SIGNING-KEY"

	bookedEventTypeURI = "https://api.calendly.com/event_types/TYPE0001"
	otherEventTypeURI  = "https://api.calendly.com/event_types/TYPE0002"
)

// bookingBinding is the eventTypePicker binding Dex Web saves: only the booked event type starts a Flow.
var bookingBinding = calendly.InviteeEventReceivedTriggerConfiguration{EventTypeURI: bookedEventTypeURI}

// newExampleConnection builds the token connection Dex Web saves; a static credential replaces project storage.
func newExampleConnection(t *testing.T, options ...calendly.Option) calendly.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "calendly", Name: inviteerecorder.ConnectionName}
	client, err := calendly.New(calendly.Config{}, sdkgo.StaticCredentialProvider[calendly.Credentials]{reference: {
		AuthMethodID: calendly.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
		WebhookSigningKey: sdkgo.NewSecretString(sentinelSigningKey),
	}}, options...)
	require.NoError(t, err)
	connection, err := calendly.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// inviteeEndpoint serves the example's target like newInviteeEndpointRunner, without the durable project inbox.
type inviteeEndpoint struct {
	server         *httptest.Server
	endpointRunner *webhooktrigger.EndpointRunner
	readiness      interface{ RunningSourceCount() int }
}

func newInviteeEndpoint(t *testing.T, connection calendly.Connection, target sdkgo.TriggerTarget[calendly.InviteeEvent]) *inviteeEndpoint {
	t.Helper()
	handler, err := connection.InviteeEventReceivedWebhookHandler()
	require.NoError(t, err)
	trigger := calendly.NewInviteeEventReceivedTrigger(calendly.InviteeEventReceivedTriggerConfig{
		Connection: connection, ConnectionName: inviteerecorder.ConnectionName,
		BindingName: inviteerecorder.InviteeCreatedTriggerBinding, Configuration: bookingBinding, Target: target,
	})
	endpointRunner, err := webhooktrigger.NewEndpointRunner(handler, trigger)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(webhookPath, endpointRunner)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &inviteeEndpoint{server: server, endpointRunner: endpointRunner, readiness: handler.(interface{ RunningSourceCount() int })}
}

// start runs the binding until the test ends and waits until it receives deliveries.
func (endpoint *inviteeEndpoint) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- endpoint.endpointRunner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	require.Eventually(t, func() bool { return endpoint.readiness.RunningSourceCount() == 1 }, 10*time.Second, 10*time.Millisecond,
		"the Calendly binding must start receiving")
}

// deliverWebhook posts body as Calendly does, signed now with signingKey; tamper changes it after signing.
func (endpoint *inviteeEndpoint) deliverWebhook(t *testing.T, body string, signingKey string, isTampered bool) int {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(timestamp + "." + body))
	if isTampered {
		body = strings.Replace(body, "ada@", "eve@", 1)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint.server.URL+webhookPath, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Calendly-Webhook-Signature", "t="+timestamp+",v1="+hex.EncodeToString(mac.Sum(nil)))
	response, err := endpoint.server.Client().Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), sentinelSigningKey)
	return response.StatusCode
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
