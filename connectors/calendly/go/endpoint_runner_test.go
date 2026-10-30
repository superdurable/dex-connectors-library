// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

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
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/connectors/calendly/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// endpointRunnerScope is the project scope of the bindings' durable inboxes, which the tests keep in memory.
var endpointRunnerScope = projectconfig.Scope{ProjectID: "calendly-endpoint-runner-tests", Kind: "live"}

// bookingsInboxKey identifies the durable inbox of the tests' bookings binding.
var bookingsInboxKey = projectconfig.TriggerInboxKey{
	ConnectorID: calendly.ConnectorID, ConnectionName: testConnection.Name, TriggerName: "inviteeEventReceived", BindingName: "bookings",
}

// bookingsConfiguration is the project configuration Dex Web saves for the bookings binding.
func bookingsConfiguration(bindingConfiguration string) projectconfig.Configuration {
	return projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: bookingsInboxKey.ConnectorID, ConnectionName: bookingsInboxKey.ConnectionName,
		TriggerName: bookingsInboxKey.TriggerName, BindingName: bookingsInboxKey.BindingName,
		Configuration: json.RawMessage(bindingConfiguration),
	}}}
}

// projectInboxes wraps each binding's target in a durable inbox kept in in-memory project storage.
type projectInboxes struct {
	objects *testsupport.ObjectStore
}

func (inboxes projectInboxes) makeDurable(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[calendly.InviteeEvent],
) (sdkgo.TriggerTarget[calendly.InviteeEvent], error) {
	inbox, err := projectconfig.NewTriggerInbox(inboxes.objects, endpointRunnerScope, key)
	if err != nil {
		return nil, err
	}
	return provider.NewDurableTriggerTarget(inbox, key, target)
}

func (inboxes projectInboxes) pendingEventIDs(t *testing.T) []string {
	t.Helper()
	inbox, err := projectconfig.NewTriggerInbox(inboxes.objects, endpointRunnerScope, bookingsInboxKey)
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
	runner *calendly.InviteeEventReceivedEndpointRunner
	cancel context.CancelFunc
	result chan error
}

func startEndpointRunner(
	t *testing.T, configuration projectconfig.Configuration, inboxes projectInboxes, target sdkgo.TriggerTarget[calendly.InviteeEvent],
) *runningEndpointRunner {
	t.Helper()
	connection, err := calendly.NewConnection(newTestClient(t, nil, personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{}), testConnection)
	require.NoError(t, err)
	runner, err := calendly.NewInviteeEventReceivedEndpointRunnerForTest(connection, configuration,
		[]calendly.ProjectInviteeEventReceivedTriggerRoute{{BindingName: bookingsInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
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
	request := httptest.NewRequest(http.MethodPost, "/webhooks/calendly", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Calendly-Webhook-Signature", calendlySignature(sentinelSigningKey, fixedNow, body))
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
	configuration := bookingsConfiguration(`{"eventTypeUri":"` + testEventTypeURI + `"}`)
	unreachable := sdkgo.TriggerTargetFunc[calendly.InviteeEvent](func(context.Context, sdkgo.TriggerEvent[calendly.InviteeEvent]) error {
		return errors.New("the Dex Server is unreachable")
	})
	booked := "invitee.created:EVENT0001:INVITEE01"

	firstRun := startEndpointRunner(t, configuration, inboxes, unreachable)
	require.Equal(t, http.StatusOK, firstRun.deliver(t, inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "INVITEE01", testEventTypeURI)))
	require.Equal(t, []string{booked}, inboxes.pendingEventIDs(t), "the 200 came after the inbox write")
	require.Equal(t, http.StatusOK, firstRun.deliver(t, inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "INVITEE02",
		"https://api.calendly.com/event_types/TYPE0002")), "the binding filters another event type but still acknowledges it")
	require.Equal(t, []string{booked}, inboxes.pendingEventIDs(t))
	firstRun.stop(t)
	require.Equal(t, []string{booked}, inboxes.pendingEventIDs(t), "an undelivered event survives the restart")

	delivered := make(chan string, 1)
	secondRun := startEndpointRunner(t, configuration, inboxes,
		sdkgo.TriggerTargetFunc[calendly.InviteeEvent](func(_ context.Context, event sdkgo.TriggerEvent[calendly.InviteeEvent]) error {
			delivered <- event.ID
			return nil
		}))
	select {
	case eventID := <-delivered:
		require.Equal(t, booked, eventID)
	case <-time.After(5 * time.Second):
		t.Fatal("the restarted runner did not replay the stored event")
	}
	require.Eventually(t, func() bool { return len(inboxes.pendingEventIDs(t)) == 0 }, 5*time.Second, 10*time.Millisecond)
	secondRun.stop(t)
}

func TestProjectEndpointRunnerRequiresEachRouteBindingInTheProjectConfiguration(t *testing.T) {
	connection, err := calendly.NewConnection(newTestClient(t, nil, personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{}), testConnection)
	require.NoError(t, err)
	target := sdkgo.TriggerTargetFunc[calendly.InviteeEvent](func(context.Context, sdkgo.TriggerEvent[calendly.InviteeEvent]) error { return nil })
	inboxes := projectInboxes{objects: testsupport.NewObjectStore()}

	_, err = calendly.NewInviteeEventReceivedEndpointRunnerForTest(connection, projectconfig.Configuration{},
		[]calendly.ProjectInviteeEventReceivedTriggerRoute{{BindingName: bookingsInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	require.ErrorContains(t, err, `binding "bookings"`)

	_, err = calendly.NewInviteeEventReceivedEndpointRunnerForTest(connection, bookingsConfiguration(`{"eventTypeUri":"not-a-uri"}`),
		[]calendly.ProjectInviteeEventReceivedTriggerRoute{{BindingName: bookingsInboxKey.BindingName, Target: target}}, inboxes.makeDurable)
	require.ErrorContains(t, err, `binding "bookings"`)
}
