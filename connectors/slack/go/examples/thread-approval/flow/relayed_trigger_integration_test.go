//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadapproval

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/connectors/slack/internal/testlog"
	"github.com/superdurable/dex-connectors-library/connectors/slack/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/projectconfigtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sentinelScopeToken = "SENTINEL-SCOPE-TOKEN"

// TestThreadApprovalRelayedRunnerStartsOneFlowPerEventWithRealDex runs the example's Flow from events that a host
// relays for a shared connection without an app_token, posted over HTTP to the runner that main.go mounts. A
// relayed event that arrives twice, while the first copy is pending and after it was consumed, starts one Flow.
func TestThreadApprovalRelayedRunnerStartsOneFlowPerEventWithRealDex(t *testing.T) {
	fake := newSocketModeSlack(t)
	flow, harness := newSlackIntegrationHarness(t, fake.URL)
	harness.startWorker(t)
	host := newRelayingHost(t)
	logs := testlog.NewLogRecorder()
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured Trigger logs:\n%s", logs.TextHandlerOutput())
		}
	})
	project := host.loadProject(t, fake.URL)
	runner := newRelayedThreadApprovalRunner(t, project, harness.client, flow, logs)
	mux := http.NewServeMux()
	mux.Handle(slack.RelayedTriggerPath(ConnectionName), runner)
	application := httptest.NewServer(mux)
	t.Cleanup(application.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runContext, stopRunner := context.WithCancel(ctx)
	runResult := make(chan error, 1)
	go func() { runResult <- runner.Run(runContext) }()
	host.waitForRegistration(t)

	teamID := "T" + strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := fmt.Sprintf("slack-thread-approval-%s-C1-1.0", teamID)
	rootID, approvalID := "EvRoot-"+teamID, "EvApprove-"+teamID
	root := relayedCallback(teamID, rootID, socketModeMessage("1.0", "", "U1", sentinelText+" request approval"))
	// The two copies race, so the second usually arrives while the first is pending in the start inbox.
	statuses := make(chan int, 2)
	var posts sync.WaitGroup
	for range 2 {
		posts.Add(1)
		go func() {
			defer posts.Done()
			statuses <- host.relay(application.URL, root)
		}()
	}
	posts.Wait()
	require.Equal(t, http.StatusOK, <-statuses)
	require.Equal(t, http.StatusOK, <-statuses)
	waitForSlackStatus(t, ctx, harness.client, flow, flowID, StatusWaitingForReply)
	require.Equal(t, http.StatusOK, host.relay(application.URL,
		relayedCallback(teamID, approvalID, socketModeMessage("1.1", "1.0", "U2", sentinelText+" approve"))))
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)

	duplicateStart := map[string]string{"event_id": rootID, "duplicate": "true"}
	// The racing copy was either ignored by the inbox while the first was pending, or refused by Dex.
	earlierDuplicates := len(logs.Find("trigger event delivered", duplicateStart))
	require.LessOrEqual(t, earlierDuplicates, 1)
	require.Equal(t, http.StatusOK, host.relay(application.URL, root), "a redelivery after completion is acknowledged")
	require.Eventually(t, func() bool {
		return len(logs.Find("trigger event delivered", duplicateStart)) == earlierDuplicates+1
	}, slackRunnerDeadline, 50*time.Millisecond, "Dex refuses a second start of the consumed event")
	require.Len(t, logs.Find("trigger event delivered", map[string]string{"event_id": rootID, "duplicate": "false"}), 1,
		"the event started exactly one Flow")
	require.Equal(t, 1, fake.postCount("1.0"))

	stopRunner()
	select {
	case err := <-runResult:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(slackRunnerDeadline):
		t.Fatal("relayed Slack Trigger runner did not stop after cancellation")
	}
	require.NotContains(t, logs.Text(), "SENTINEL")
}

// newRelayedThreadApprovalRunner wires the runner as main.go does, through NewProjectMessageTriggerRunner.
func newRelayedThreadApprovalRunner(
	t *testing.T, project *projectconfig.LoadedProject, client *dex.Client, flow *Flow, logs *testlog.LogRecorder,
) *slack.MessageTriggerRunner {
	t.Helper()
	var startConfiguration slack.ChannelThreadCreatedTriggerConfiguration
	require.NoError(t, project.Configuration.DecodeTriggerConfiguration(
		slack.ConnectorID, ConnectionName, slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, StartTriggerBinding, &startConfiguration,
	))
	startFilter, err := NewStartTriggerFilter(startConfiguration)
	require.NoError(t, err)
	var replyConfiguration slack.ThreadReplyCreatedTriggerConfiguration
	require.NoError(t, project.Configuration.DecodeTriggerConfiguration(
		slack.ConnectorID, ConnectionName, slack.ThreadReplyCreatedTriggerDefinition.Trigger.TriggerName, ReplyTriggerBinding, &replyConfiguration,
	))
	replyFilter, err := NewReplyTriggerFilter(replyConfiguration)
	require.NoError(t, err)
	logger := logs.Logger()
	runner, err := slack.NewProjectMessageTriggerRunner(project, ConnectionName, slack.ProjectMessageTriggerRunnerConfig{
		ChannelThreadCreatedRoutes: []slack.ProjectChannelThreadCreatedTriggerRoute{{
			BindingName: StartTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(client, flow, startFilter, ResolveFlowID, MapToFlowInput,
				sdkgo.WithTriggerLogger(logger)),
		}},
		ThreadReplyCreatedRoutes: []slack.ProjectThreadReplyCreatedTriggerRoute{{
			BindingName: ReplyTriggerBinding,
			Target: sdkgo.NewDexRPCTriggerTarget(client, flow.ReceiveThreadReply, replyFilter, ResolveFlowID,
				MapToReceiveThreadReplyInput, sdkgo.WithTriggerLogger(logger)),
		}},
	}, slack.WithLogger(logger))
	require.NoError(t, err)
	return runner
}

// relayingHost plays the host that relays Slack events: its credential broker serves the shared connection's
// credential without an app_token and stores registrations, and it signs each relayed callback with the scope
// token.
type relayingHost struct {
	broker        *httptest.Server
	objects       *testsupport.ObjectStore
	registrations chan map[string]any
}

func newRelayingHost(t *testing.T) *relayingHost {
	t.Helper()
	host := &relayingHost{objects: testsupport.NewObjectStore(), registrations: make(chan map[string]any, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/connection-credentials/read", func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{
			"status": "READY", "revision": 1,
			"credential": map[string]string{"bot_token": sentinelBotToken, "user_token": sentinelUserToken},
		})
	})
	mux.HandleFunc("/internal/slack-triggers/register", func(response http.ResponseWriter, request *http.Request) {
		var registration map[string]any
		contents, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(contents, &registration)
		registration["authorization"] = request.Header.Get("Authorization")
		host.registrations <- registration
		_, _ = response.Write([]byte(`{"status":"REGISTERED"}`))
	})
	host.broker = httptest.NewServer(mux)
	t.Cleanup(host.broker.Close)
	return host
}

// loadProject writes a configuration whose Slack connection is shared and loads it as an application would.
func (host *relayingHost) loadProject(t *testing.T, slackEndpoint string) *projectconfig.LoadedProject {
	t.Helper()
	ctx := context.Background()
	configurations, err := projectconfig.NewConfigurationStore(&projectconfig.ConfigurationStoreConfig{Objects: host.objects, Scope: threadApprovalScope})
	require.NoError(t, err)
	token, err := configurations.CreateCredentialBrokerToken(ctx, sentinelScopeToken)
	require.NoError(t, err)
	settings, err := json.Marshal(map[string]string{"endpoint": slackEndpoint})
	require.NoError(t, err)
	_, _, err = configurations.WriteConfiguration(ctx, 0, projectconfig.Configuration{
		Connections: []projectconfig.ConnectionConfiguration{{
			ConnectorID: slack.ConnectorID, ConnectionName: ConnectionName, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/slack",
			Provider: "slack", SharedConnectionID: "conn-slack", Configuration: settings,
		}},
		TriggerBindings:  newThreadApprovalBindings(),
		CredentialBroker: &projectconfig.CredentialBrokerConfiguration{Endpoint: host.broker.URL + "/internal/connection-credentials/read", TokenSecretRef: token},
	})
	require.NoError(t, err)
	reference, err := configurations.FreezeConfiguration(ctx, 1)
	require.NoError(t, err)
	project, err := projectconfigtest.LoadProject(ctx, host.objects, threadApprovalScope, reference)
	require.NoError(t, err)
	return project
}

func (host *relayingHost) waitForRegistration(t *testing.T) {
	t.Helper()
	select {
	case registration := <-host.registrations:
		require.Equal(t, map[string]any{
			"sharedConnectionId": "conn-slack", "connectionName": ConnectionName, "authorization": "Bearer " + sentinelScopeToken,
		}, registration)
	case <-time.After(slackRunnerDeadline):
		t.Fatal("the relayed runner did not register its connection name")
	}
}

// relay posts body to the application's relayed Trigger path, signed as the host signs it, and returns the status.
// A transport failure returns 0, which the caller's status check reports.
func (host *relayingHost) relay(applicationURL string, body []byte) int {
	request, err := http.NewRequest(http.MethodPost, applicationURL+slack.RelayedTriggerPath(ConnectionName), bytes.NewReader(body))
	if err != nil {
		return 0
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(sentinelScopeToken))
	_, _ = mac.Write([]byte("v1:" + timestamp + ":"))
	_, _ = mac.Write(body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(projectconfig.RelayTimestampHeader, timestamp)
	request.Header.Set(projectconfig.RelaySignatureHeader, "v1="+hex.EncodeToString(mac.Sum(nil)))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0
	}
	if response.Body.Close() != nil {
		return 0
	}
	return response.StatusCode
}

// relayedCallback is the body a host relays: Slack's event_id, event_time, outer team_id and inner event.
func relayedCallback(teamID string, eventID string, message map[string]any) []byte {
	body, err := json.Marshal(map[string]any{"event_id": eventID, "event_time": time.Now().Unix(), "team_id": teamID, "event": message})
	if err != nil {
		panic(err)
	}
	return body
}
