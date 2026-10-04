// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// endpointRunnerScope is the project scope of the bindings' durable inboxes, which the tests keep in memory.
var endpointRunnerScope = projectconfig.Scope{ProjectID: "helpscout-endpoint-runner-tests", Kind: "live"}

// conversationsInboxKey identifies the durable inbox of the tests' conversations binding.
var conversationsInboxKey = projectconfig.TriggerInboxKey{
	ConnectorID: helpscout.ConnectorID, ConnectionName: testConnection.Name, TriggerName: "conversationEvent", BindingName: "conversations",
}

// conversationsConfiguration is the project configuration Dex Web saves for the conversations binding.
func conversationsConfiguration(bindingConfiguration string) projectconfig.Configuration {
	return projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: conversationsInboxKey.ConnectorID, ConnectionName: conversationsInboxKey.ConnectionName,
		TriggerName: conversationsInboxKey.TriggerName, BindingName: conversationsInboxKey.BindingName,
		Configuration: json.RawMessage(bindingConfiguration),
	}}}
}

// projectInboxes wraps each binding's target in a durable inbox kept in in-memory project storage.
type projectInboxes struct {
	objects *testsupport.ObjectStore
}

func (inboxes projectInboxes) makeDurable(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[helpscout.ConversationEvent],
) (sdkgo.TriggerTarget[helpscout.ConversationEvent], error) {
	inbox, err := projectconfig.NewTriggerInbox(inboxes.objects, endpointRunnerScope, key)
	if err != nil {
		return nil, err
	}
	return provider.NewDurableTriggerTarget(inbox, key, target)
}

func (inboxes projectInboxes) pendingEventIDs(t *testing.T) []string {
	t.Helper()
	inbox, err := projectconfig.NewTriggerInbox(inboxes.objects, endpointRunnerScope, conversationsInboxKey)
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
	runner *helpscout.ConversationEventEndpointRunner
	cancel context.CancelFunc
	result chan error
}

func startEndpointRunner(
	t *testing.T, configuration projectconfig.Configuration, inboxes projectInboxes, target sdkgo.TriggerTarget[helpscout.ConversationEvent],
) *runningEndpointRunner {
	t.Helper()
	connection, err := helpscout.NewConnection(newTestClient(t, nil, staticCredentials(sentinelWebhookSecret), helpscout.Config{}), testConnection)
	require.NoError(t, err)
	runner, err := helpscout.NewConversationEventEndpointRunnerForTest(connection, configuration,
		[]helpscout.ProjectConversationEventTriggerRoute{{BindingName: conversationsInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
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
	request := httptest.NewRequest(http.MethodPost, "/webhooks/helpscout", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-HelpScout-Event", helpscout.WebhookEventConversationCreated)
	request.Header.Set("X-HelpScout-Signature", helpScoutSignature(sentinelWebhookSecret, body))
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
	configuration := conversationsConfiguration(`{"mailboxId":123}`)
	unreachable := sdkgo.TriggerTargetFunc[helpscout.ConversationEvent](func(context.Context, sdkgo.TriggerEvent[helpscout.ConversationEvent]) error {
		return errors.New("the Dex Server is unreachable")
	})

	firstRun := startEndpointRunner(t, configuration, inboxes, unreachable)
	require.Equal(t, http.StatusOK, firstRun.deliver(t, conversationJSON(501, "active", 123)))
	pending := inboxes.pendingEventIDs(t)
	require.Len(t, pending, 1, "the 200 came after the inbox write")
	require.Regexp(t, `^convo\.created:501:[0-9a-f]{32}$`, pending[0])
	require.Equal(t, http.StatusOK, firstRun.deliver(t, conversationJSON(502, "active", 456)),
		"the binding filters another inbox but still acknowledges it")
	require.Equal(t, pending, inboxes.pendingEventIDs(t))
	firstRun.stop(t)
	require.Equal(t, pending, inboxes.pendingEventIDs(t), "an undelivered event survives the restart")

	delivered := make(chan string, 1)
	secondRun := startEndpointRunner(t, configuration, inboxes,
		sdkgo.TriggerTargetFunc[helpscout.ConversationEvent](func(_ context.Context, event sdkgo.TriggerEvent[helpscout.ConversationEvent]) error {
			delivered <- event.ID
			return nil
		}))
	select {
	case eventID := <-delivered:
		require.Equal(t, pending[0], eventID)
	case <-time.After(5 * time.Second):
		t.Fatal("the restarted runner did not replay the stored event")
	}
	require.Eventually(t, func() bool { return len(inboxes.pendingEventIDs(t)) == 0 }, 5*time.Second, 10*time.Millisecond)
	secondRun.stop(t)
}

func TestProjectEndpointRunnerRequiresEachRouteBindingInTheProjectConfiguration(t *testing.T) {
	connection, err := helpscout.NewConnection(newTestClient(t, nil, staticCredentials(sentinelWebhookSecret), helpscout.Config{}), testConnection)
	require.NoError(t, err)
	target := sdkgo.TriggerTargetFunc[helpscout.ConversationEvent](func(context.Context, sdkgo.TriggerEvent[helpscout.ConversationEvent]) error {
		return nil
	})
	inboxes := projectInboxes{objects: testsupport.NewObjectStore()}

	_, err = helpscout.NewConversationEventEndpointRunnerForTest(connection, projectconfig.Configuration{},
		[]helpscout.ProjectConversationEventTriggerRoute{{BindingName: conversationsInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	require.ErrorContains(t, err, `binding "conversations"`)

	_, err = helpscout.NewConversationEventEndpointRunnerForTest(connection, conversationsConfiguration(`{"mailboxId":-1}`),
		[]helpscout.ProjectConversationEventTriggerRoute{{BindingName: conversationsInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
	require.ErrorContains(t, err, `binding "conversations"`)
}
