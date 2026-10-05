// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/connectors/linear/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// endpointRunnerScope is the project scope of the bindings' durable inboxes, which the tests keep in memory.
var endpointRunnerScope = projectconfig.Scope{ProjectID: "linear-endpoint-runner-tests", Kind: "live"}

// issuesInboxKey identifies the durable inbox of the tests' issues binding.
var issuesInboxKey = projectconfig.TriggerInboxKey{
	ConnectorID: linear.ConnectorID, ConnectionName: linearConnection.Name, TriggerName: "issueEventReceived", BindingName: "issues",
}

// issuesConfiguration is the project configuration Dex Web saves for the issues binding.
func issuesConfiguration(bindingConfiguration string) projectconfig.Configuration {
	return projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: issuesInboxKey.ConnectorID, ConnectionName: issuesInboxKey.ConnectionName,
		TriggerName: issuesInboxKey.TriggerName, BindingName: issuesInboxKey.BindingName,
		Configuration: json.RawMessage(bindingConfiguration),
	}}}
}

// projectInboxes wraps each binding's target in a durable inbox kept in in-memory project storage.
type projectInboxes struct {
	objects *testsupport.ObjectStore
}

func (inboxes projectInboxes) makeDurable(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[linear.IssueEvent],
) (sdkgo.TriggerTarget[linear.IssueEvent], error) {
	inbox, err := projectconfig.NewTriggerInbox(inboxes.objects, endpointRunnerScope, key)
	if err != nil {
		return nil, err
	}
	return provider.NewDurableTriggerTarget(inbox, key, target)
}

func (inboxes projectInboxes) pendingEventIDs(t *testing.T) []string {
	t.Helper()
	inbox, err := projectconfig.NewTriggerInbox(inboxes.objects, endpointRunnerScope, issuesInboxKey)
	require.NoError(t, err)
	pending, err := inbox.Pending(context.Background())
	require.NoError(t, err)
	eventIDs := []string{}
	for _, event := range pending {
		eventIDs = append(eventIDs, event.ID)
	}
	return eventIDs
}

// runningEndpointRunner is one project endpoint runner that runs until stop or the end of the test.
type runningEndpointRunner struct {
	runner *linear.IssueEventReceivedEndpointRunner
	cancel context.CancelFunc
	result chan error
}

func newWebhookConnection(t *testing.T) linear.Connection {
	t.Helper()
	client, err := linear.New(linear.Config{}, apiKeyCredentials(), linear.WithClock(func() time.Time { return webhookNow }))
	require.NoError(t, err)
	connection, err := linear.NewConnection(client, linearConnection)
	require.NoError(t, err)
	return connection
}

func startEndpointRunner(
	t *testing.T, configuration projectconfig.Configuration, inboxes projectInboxes, target sdkgo.TriggerTarget[linear.IssueEvent],
) *runningEndpointRunner {
	t.Helper()
	runner, err := linear.NewIssueEventReceivedEndpointRunnerForTest(newWebhookConnection(t), configuration,
		[]linear.ProjectIssueEventReceivedTriggerRoute{{BindingName: issuesInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	running := &runningEndpointRunner{runner: runner, cancel: cancel, result: make(chan error, 1)}
	go func() { running.result <- runner.Run(ctx) }()
	t.Cleanup(cancel)
	require.Eventually(t, func() bool { return runner.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	return running
}

func (running *runningEndpointRunner) deliver(t *testing.T, body string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Linear-Signature", linearSignature(testSigningSecret, body))
	response := httptest.NewRecorder()
	running.runner.ServeHTTP(response, request)
	return response.Code
}

func (running *runningEndpointRunner) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	select {
	case err := <-running.result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("the endpoint runner did not stop after cancellation")
	}
}

func TestProjectEndpointRunnerStoresEachEventBeforeAcknowledgingAndReplaysItAfterARestart(t *testing.T) {
	inboxes := projectInboxes{objects: testsupport.NewObjectStore()}
	configuration := issuesConfiguration(`{"teamId":"` + testTeamID + `","actions":["create"]}`)
	unreachable := sdkgo.TriggerTargetFunc[linear.IssueEvent](func(context.Context, sdkgo.TriggerEvent[linear.IssueEvent]) error {
		return errors.New("the Dex Server is unreachable")
	})
	updatedAt := time.Date(2026, time.October, 4, 11, 59, 58, 0, time.UTC)
	created := fmt.Sprintf("create:%s:%d", testIssueID, updatedAt.UnixMilli())

	firstRun := startEndpointRunner(t, configuration, inboxes, unreachable)
	require.Equal(t, http.StatusOK, firstRun.deliver(t, issueWebhookBody("Issue", "create", testTeamID, "2026-10-04T11:59:58.000Z", webhookNow)))
	require.Equal(t, []string{created}, inboxes.pendingEventIDs(t), "the 200 came after the inbox write")
	require.Equal(t, http.StatusOK, firstRun.deliver(t, issueWebhookBody("Issue", "update", testTeamID, "2026-10-04T11:59:59.000Z", webhookNow)),
		"the binding filters an update but still acknowledges it")
	require.Equal(t, []string{created}, inboxes.pendingEventIDs(t))
	firstRun.stop(t)
	require.Equal(t, []string{created}, inboxes.pendingEventIDs(t), "an undelivered event survives the restart")

	delivered := make(chan string, 1)
	secondRun := startEndpointRunner(t, configuration, inboxes,
		sdkgo.TriggerTargetFunc[linear.IssueEvent](func(_ context.Context, event sdkgo.TriggerEvent[linear.IssueEvent]) error {
			delivered <- event.ID
			return nil
		}))
	select {
	case eventID := <-delivered:
		require.Equal(t, created, eventID)
	case <-time.After(5 * time.Second):
		t.Fatal("the restarted runner did not replay the stored event")
	}
	require.Eventually(t, func() bool { return len(inboxes.pendingEventIDs(t)) == 0 }, 5*time.Second, 10*time.Millisecond)
	secondRun.stop(t)
}

func TestProjectEndpointRunnerRequiresEachRouteBindingInTheProjectConfiguration(t *testing.T) {
	connection := newWebhookConnection(t)
	target := sdkgo.TriggerTargetFunc[linear.IssueEvent](func(context.Context, sdkgo.TriggerEvent[linear.IssueEvent]) error { return nil })
	inboxes := projectInboxes{objects: testsupport.NewObjectStore()}

	_, err := linear.NewIssueEventReceivedEndpointRunnerForTest(connection, projectconfig.Configuration{},
		[]linear.ProjectIssueEventReceivedTriggerRoute{{BindingName: issuesInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	require.ErrorContains(t, err, `binding "issues"`)

	_, err = linear.NewIssueEventReceivedEndpointRunnerForTest(connection, issuesConfiguration(`{"teamId":"ENG"}`),
		[]linear.ProjectIssueEventReceivedTriggerRoute{{BindingName: issuesInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
	require.ErrorContains(t, err, `binding "issues"`)

	_, err = linear.NewIssueEventReceivedEndpointRunnerForTest(connection, issuesConfiguration(`{}`), nil, inboxes.makeDurable)
	require.ErrorContains(t, err, "routes are required")
}
