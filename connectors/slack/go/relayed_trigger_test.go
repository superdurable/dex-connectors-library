// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/internal/testlog"
	"github.com/superdurable/dex-connectors-library/connectors/slack/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/projectconfigtest"
)

const (
	relayScopeToken         = "SENTINEL-SCOPE-TOKEN"
	relayCredentialReadPath = "/internal/connection-credentials/read"
	relaySharedConnectionID = "conn-slack"
)

func TestRelayedTriggerPathIsTheUnpaddedBase64URLConnectionName(t *testing.T) {
	require.Equal(t, "/triggers/slack/c3VwcG9ydA", RelayedTriggerPath("support"))
	require.Equal(t, "/triggers/slack/c3VwcG9ydC10ZWFtLz8", RelayedTriggerPath("support-team/?"))
}

func TestRelayedRunnerRegistersThenRecordsAndDeliversRootBeforeReply(t *testing.T) {
	host := newRelayHost(t, nil)
	routes := newRelayRoutes(nil)
	runner := host.newRunner(t, routes.config())
	require.Equal(t, http.StatusServiceUnavailable, host.post(t, runner, rootMessageBody("EvEarly", "1.0"), time.Now()).Code,
		"the receiver refuses events until Run serves them")

	stop := host.run(t, runner)
	defer stop()
	registration := host.waitForRegistration(t)
	require.Equal(t, "Bearer "+relayScopeToken, registration.authorization)
	require.Equal(t, map[string]any{"sharedConnectionId": relaySharedConnectionID, "connectionName": "workspace"}, registration.body)
	require.Len(t, host.logs.Find("slack relayed trigger registered", map[string]string{"connection": "workspace"}), 1)

	require.Equal(t, http.StatusOK, host.post(t, runner, rootMessageBody("EvRoot", "1.0"), time.Now()).Code)
	require.Equal(t, http.StatusOK, host.post(t, runner, replyMessageBody("EvReply", "1.1", "1.0"), time.Now()).Code)
	require.Equal(t, []string{"root:EvRoot", "reply:EvReply"}, routes.waitForDeliveries(t, 2), "a reply never overtakes its root")
	root := routes.event("EvRoot")
	require.Equal(t, MessageEvent{TeamID: "T1", ChannelID: "C1", Timestamp: "1.0", ThreadTimestamp: "1.0", UserID: "U1", Text: sentinelText + " request approval"}, root.Payload)
	require.Equal(t, time.Unix(1760227200, 0).UTC(), root.OccurredAt)
	require.Eventually(t, func() bool { return len(host.pendingEventIDs(t)) == 0 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, host.credentialReads(), "the mode is chosen from one credential read")
	requireNoSentinel(t, host.logs)
}

func TestRelayedRunnerRefusesUnsignedStaleAndOversizedRequests(t *testing.T) {
	host := newRelayHost(t, nil)
	routes := newRelayRoutes(nil)
	runner := host.newRunner(t, routes.config())
	stop := host.run(t, runner)
	defer stop()
	host.waitForRegistration(t)
	body := rootMessageBody("EvRoot", "1.0")

	wrongKey := host.request(body, time.Now())
	wrongKey.Header.Set(projectconfig.RelaySignatureHeader, relaySignature("another-token", wrongKey.Header.Get(projectconfig.RelayTimestampHeader), body))
	require.Equal(t, http.StatusBadRequest, serve(runner, wrongKey).Code)
	require.Equal(t, http.StatusBadRequest, host.post(t, runner, body, time.Now().Add(-6*time.Minute)).Code)
	unsigned := host.request(body, time.Now())
	unsigned.Header.Del(projectconfig.RelaySignatureHeader)
	require.Equal(t, http.StatusBadRequest, serve(runner, unsigned).Code)
	tampered := host.request(body, time.Now())
	tampered.Body = io.NopCloser(strings.NewReader(string(rootMessageBody("EvForged", "1.0"))))
	require.Equal(t, http.StatusBadRequest, serve(runner, tampered).Code)
	oversized := append([]byte(`{"event_id":"EvLarge","pad":"`), bytes.Repeat([]byte("x"), maxRelayedEventBytes)...)
	require.Equal(t, http.StatusRequestEntityTooLarge, host.post(t, runner, append(oversized, '"', '}'), time.Now()).Code)
	require.Equal(t, http.StatusOK, host.post(t, runner, rootMessageBody("EvOtherChannel", "2.0", "C9"), time.Now()).Code,
		"an event no binding accepts is acknowledged")

	require.Empty(t, host.pendingEventIDs(t))
	require.Empty(t, routes.deliveries())
	refusals := host.logs.Find("slack relayed request refused", map[string]string{"reason": "invalid_signature"})
	require.Len(t, refusals, 4)
	require.Equal(t, slog.LevelDebug, refusals[0].Level, "anyone can post to the public route, so a bad signature is not a warning")
	requireNoSentinel(t, host.logs)
}

func TestRelayedRunnerRetriesAnUnavailableHostAndStopsOnARefusal(t *testing.T) {
	shortenSocketModeTimings(t, time.Millisecond, 4*time.Millisecond, socketModeReadTimeout)
	host := newRelayHost(t, func(attempt int) (int, string) {
		if attempt < 3 {
			return http.StatusServiceUnavailable, ""
		}
		return http.StatusOK, `{"status":"REGISTERED"}`
	})
	runner := host.newRunner(t, newRelayRoutes(nil).config())
	stop := host.run(t, runner)
	defer stop()
	host.waitForRegistrations(t, 3)
	require.Len(t, host.logs.Find("slack relayed trigger registration failed; retrying", nil), 2)

	for status, answer := range map[string]func(int) (int, string){
		"not relayed":    func(int) (int, string) { return http.StatusOK, `{"status":"NOT_RELAYED"}` },
		"not selected":   func(int) (int, string) { return http.StatusOK, `{"status":"CONNECTION_NOT_SELECTED"}` },
		"too many names": func(int) (int, string) { return http.StatusConflict, "" },
	} {
		t.Run(status, func(t *testing.T) {
			refused := newRelayHost(t, answer)
			runner := refused.newRunner(t, newRelayRoutes(nil).config())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := runner.Run(ctx)
			require.Error(t, err)
			require.NotErrorIs(t, err, context.DeadlineExceeded)
			require.NotContains(t, err.Error(), relayScopeToken)
			require.Equal(t, http.StatusServiceUnavailable, refused.post(t, runner, rootMessageBody("EvRoot", "1.0"), time.Now()).Code,
				"a stopped runner receives nothing")
		})
	}
}

func TestProjectRunnerUsesSocketModeWhenTheSharedCredentialHasAnAppToken(t *testing.T) {
	host := newRelayHost(t, nil)
	host.credential = map[string]string{"bot_token": sentinelBotToken, "user_token": sentinelUserToken, "app_token": sentinelAppToken}
	runner := host.newRunner(t, newRelayRoutes(nil).config())
	stop := host.run(t, runner)
	defer stop()
	require.Eventually(t, func() bool {
		return len(host.logs.Find("slack socket mode connection failed; reconnecting", map[string]string{
			"error": "Slack rejected the Socket Mode connection: invalid_auth (HTTP 200)",
		})) > 0
	}, 5*time.Second, 10*time.Millisecond, "the runner reads the shared credential and opens Socket Mode with its app_token")
	require.Zero(t, host.registrationCount())
	require.Equal(t, http.StatusServiceUnavailable, host.post(t, runner, rootMessageBody("EvRoot", "1.0"), time.Now()).Code)
	requireNoSentinel(t, host.logs)
}

func TestProjectRunnerStopsWhenTheConnectionIsNotSelected(t *testing.T) {
	host := newRelayHost(t, nil)
	host.credentialStatus = "CONNECTION_NOT_SELECTED"
	runner := host.newRunner(t, newRelayRoutes(nil).config())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.ErrorIs(t, runner.Run(ctx), projectconfig.ErrConnectionNotSelected)
	require.Zero(t, host.registrationCount())
}

// relayHost is the host side of the relay: its credential broker, with the credential read and the
// registration endpoint, and the project configuration it writes for one shared Slack connection.
type relayHost struct {
	broker  *httptest.Server
	slack   *httptest.Server
	objects *testsupport.ObjectStore
	logs    *testlog.LogRecorder
	answer  func(attempt int) (int, string)

	mutex            sync.Mutex
	credential       map[string]string
	credentialStatus string
	reads            int
	registrations    []relayRegistration
}

type relayRegistration struct {
	authorization string
	body          map[string]any
}

func newRelayHost(t *testing.T, answer func(attempt int) (int, string)) *relayHost {
	t.Helper()
	if answer == nil {
		answer = func(int) (int, string) { return http.StatusOK, `{"status":"REGISTERED"}` }
	}
	host := &relayHost{
		objects: testsupport.NewObjectStore(), logs: testlog.NewLogRecorder(), answer: answer, credentialStatus: "READY",
		credential: map[string]string{"bot_token": sentinelBotToken, "user_token": sentinelUserToken},
	}
	mux := http.NewServeMux()
	mux.HandleFunc(relayCredentialReadPath, host.serveCredentialRead)
	mux.HandleFunc(relayedTriggerRegistrationPath, host.serveRegistration)
	host.broker = httptest.NewServer(mux)
	t.Cleanup(host.broker.Close)
	// Socket Mode's apps.connections.open fails, so a runner that chooses Socket Mode keeps reconnecting.
	host.slack = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	t.Cleanup(host.slack.Close)
	return host
}

func (host *relayHost) serveCredentialRead(response http.ResponseWriter, _ *http.Request) {
	host.mutex.Lock()
	host.reads++
	answer := map[string]any{"status": host.credentialStatus}
	if host.credentialStatus == "READY" {
		answer["credential"], answer["revision"] = host.credential, 1
	}
	host.mutex.Unlock()
	_ = json.NewEncoder(response).Encode(answer)
}

func (host *relayHost) serveRegistration(response http.ResponseWriter, request *http.Request) {
	contents, _ := io.ReadAll(request.Body)
	registration := relayRegistration{authorization: request.Header.Get("Authorization")}
	_ = json.Unmarshal(contents, &registration.body)
	host.mutex.Lock()
	host.registrations = append(host.registrations, registration)
	statusCode, body := host.answer(len(host.registrations))
	host.mutex.Unlock()
	response.WriteHeader(statusCode)
	_, _ = response.Write([]byte(body))
}

// newRunner writes the shared connection's configuration with both bindings and builds the runner as an
// application does.
func (host *relayHost) newRunner(t *testing.T, config ProjectMessageTriggerRunnerConfig) *MessageTriggerRunner {
	t.Helper()
	ctx := context.Background()
	configurations, err := projectconfig.NewConfigurationStore(&projectconfig.ConfigurationStoreConfig{Objects: host.objects, Scope: projectSlackScope})
	require.NoError(t, err)
	token, err := configurations.CreateCredentialBrokerToken(ctx, relayScopeToken)
	require.NoError(t, err)
	settings, err := json.Marshal(map[string]string{"endpoint": host.slack.URL})
	require.NoError(t, err)
	_, _, err = configurations.WriteConfiguration(ctx, 0, projectconfig.Configuration{
		Connections: []projectconfig.ConnectionConfiguration{{
			ConnectorID: ConnectorID, ConnectionName: "workspace", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/slack",
			Provider: "slack", SharedConnectionID: relaySharedConnectionID, Configuration: settings,
		}},
		TriggerBindings:  newProjectSlackFixture(t).configuration.TriggerBindings,
		CredentialBroker: &projectconfig.CredentialBrokerConfiguration{Endpoint: host.broker.URL + relayCredentialReadPath, TokenSecretRef: token},
	})
	require.NoError(t, err)
	reference, err := configurations.FreezeConfiguration(ctx, 1)
	require.NoError(t, err)
	project, err := projectconfigtest.LoadProject(ctx, host.objects, projectSlackScope, reference)
	require.NoError(t, err)
	runner, err := NewProjectMessageTriggerRunner(project, "workspace", config, WithLogger(host.logs.Logger()))
	require.NoError(t, err)
	return runner
}

// run starts runner and returns a function that stops it and checks that it ended with the cancellation.
func (host *relayHost) run(t *testing.T, runner *MessageTriggerRunner) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(5 * time.Second):
			t.Fatal("runner did not stop")
		}
	}
}

func (host *relayHost) waitForRegistration(t *testing.T) relayRegistration {
	t.Helper()
	host.waitForRegistrations(t, 1)
	host.mutex.Lock()
	defer host.mutex.Unlock()
	return host.registrations[0]
}

func (host *relayHost) waitForRegistrations(t *testing.T, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return host.registrationCount() >= count && len(host.logs.Find("slack relayed trigger registered", nil)) > 0
	}, 5*time.Second, 5*time.Millisecond)
	require.Equal(t, count, host.registrationCount())
}

func (host *relayHost) registrationCount() int {
	host.mutex.Lock()
	defer host.mutex.Unlock()
	return len(host.registrations)
}

func (host *relayHost) credentialReads() int {
	host.mutex.Lock()
	defer host.mutex.Unlock()
	return host.reads
}

// request builds a relayed request signed with the scope token at signedAt.
func (host *relayHost) request(body []byte, signedAt time.Time) *http.Request {
	request := httptest.NewRequest(http.MethodPost, RelayedTriggerPath("workspace"), bytes.NewReader(body))
	timestamp := strconv.FormatInt(signedAt.Unix(), 10)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(projectconfig.RelayTimestampHeader, timestamp)
	request.Header.Set(projectconfig.RelaySignatureHeader, relaySignature(relayScopeToken, timestamp, body))
	return request
}

func (host *relayHost) post(t *testing.T, runner *MessageTriggerRunner, body []byte, signedAt time.Time) *httptest.ResponseRecorder {
	t.Helper()
	return serve(runner, host.request(body, signedAt))
}

func (host *relayHost) pendingEventIDs(t *testing.T) []string {
	t.Helper()
	return (&projectSlackFixture{objects: host.objects}).pendingEventIDs(t)
}

func serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func relaySignature(token string, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte("v1:" + timestamp + ":"))
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// relayRoutes records the order in which the root and reply targets receive events.
type relayRoutes struct {
	mutex     sync.Mutex
	delivered []string
	events    map[string]sdkgo.TriggerEvent[MessageEvent]
	fail      func(sdkgo.TriggerEvent[MessageEvent]) error
}

func newRelayRoutes(fail func(sdkgo.TriggerEvent[MessageEvent]) error) *relayRoutes {
	return &relayRoutes{events: map[string]sdkgo.TriggerEvent[MessageEvent]{}, fail: fail}
}

func (routes *relayRoutes) config() ProjectMessageTriggerRunnerConfig {
	return ProjectMessageTriggerRunnerConfig{
		ChannelThreadCreatedRoutes: []ProjectChannelThreadCreatedTriggerRoute{{BindingName: "approval-start", Target: routes.target("root")}},
		ThreadReplyCreatedRoutes:   []ProjectThreadReplyCreatedTriggerRoute{{BindingName: "approval-reply", Target: routes.target("reply")}},
	}
}

func (routes *relayRoutes) target(kind string) sdkgo.TriggerTarget[MessageEvent] {
	return sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
		if routes.fail != nil {
			if err := routes.fail(event); err != nil {
				return err
			}
		}
		routes.mutex.Lock()
		defer routes.mutex.Unlock()
		routes.delivered = append(routes.delivered, kind+":"+event.ID)
		routes.events[event.ID] = event
		return nil
	})
}

func (routes *relayRoutes) deliveries() []string {
	routes.mutex.Lock()
	defer routes.mutex.Unlock()
	return append([]string(nil), routes.delivered...)
}

func (routes *relayRoutes) event(eventID string) sdkgo.TriggerEvent[MessageEvent] {
	routes.mutex.Lock()
	defer routes.mutex.Unlock()
	return routes.events[eventID]
}

func (routes *relayRoutes) waitForDeliveries(t *testing.T, count int) []string {
	t.Helper()
	require.Eventually(t, func() bool { return len(routes.deliveries()) >= count }, 5*time.Second, 5*time.Millisecond)
	return routes.deliveries()
}

// rootMessageBody is a relayed Events API callback for a top-level message in channel C1, or in channel[0].
func rootMessageBody(eventID string, timestamp string, channel ...string) []byte {
	channelID := "C1"
	if len(channel) > 0 {
		channelID = channel[0]
	}
	return relayedMessageBody(eventID, messageEvent{
		Type: "message", Channel: channelID, User: "U1", Text: sentinelText + " request approval", Timestamp: timestamp,
	})
}

func replyMessageBody(eventID string, timestamp string, threadTimestamp string) []byte {
	return relayedMessageBody(eventID, messageEvent{
		Type: "message", Channel: "C1", User: "U2", Text: sentinelText + " approve", Timestamp: timestamp, ThreadTS: threadTimestamp,
	})
}

func relayedMessageBody(eventID string, message messageEvent) []byte {
	body, err := json.Marshal(eventsAPIPayload{EventID: eventID, EventTime: 1760227200, TeamID: "T1", Event: message})
	if err != nil {
		panic(err)
	}
	return body
}
