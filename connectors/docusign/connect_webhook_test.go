// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// connectBody is a JSON SIM envelope event with an envelope summary that includes custom fields.
func connectBody(event string, envelopeID string, requestID string) string {
	return `{"event":"` + event + `","apiVersion":"v2.1","uri":"/restapi/v2.1/accounts/` + testAccountID + `/envelopes/` + envelopeID + `",` +
		`"retryCount":0,"configurationId":10242474,"generatedDateTime":"2026-10-04T13:00:00.1230000Z",` +
		`"data":{"accountId":"` + testAccountID + `","userId":"4799e5e9-0000-0000-0000-cf4713bbcacc","envelopeId":"` + envelopeID + `",` +
		`"envelopeSummary":{"status":"` + strings.TrimPrefix(event, "envelope-") + `","emailSubject":"Meridian MSA",` +
		`"voidedReason":"Signer left the company","customFields":{"textCustomFields":[` +
		`{"fieldId":"1","name":"dexSigningRequestId","show":"false","value":"` + requestID + `"}]}}}}`
}

func connectSignature(key string, body string) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(body)) // A hash never fails to write.
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// collectingTarget records every delivered event.
type collectingTarget struct {
	events chan sdkgo.TriggerEvent[docusign.EnvelopeEvent]
}

func newCollectingTarget() collectingTarget {
	return collectingTarget{events: make(chan sdkgo.TriggerEvent[docusign.EnvelopeEvent], 16)}
}

func (target collectingTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[docusign.EnvelopeEvent]) error {
	target.events <- event
	return nil
}

func (target collectingTarget) next(t *testing.T) sdkgo.TriggerEvent[docusign.EnvelopeEvent] {
	t.Helper()
	select {
	case event := <-target.events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("no event was delivered")
		return sdkgo.TriggerEvent[docusign.EnvelopeEvent]{}
	}
}

// outcomesInboxKey identifies the durable inbox of the tests' outcomes binding.
var outcomesInboxKey = projectconfig.TriggerInboxKey{
	ConnectorID: docusign.ConnectorID, ConnectionName: testConnection.Name, TriggerName: "envelopeEventReceived", BindingName: "outcomes",
}

func outcomesConfiguration(bindingConfiguration string) projectconfig.Configuration {
	return projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: outcomesInboxKey.ConnectorID, ConnectionName: outcomesInboxKey.ConnectionName,
		TriggerName: outcomesInboxKey.TriggerName, BindingName: outcomesInboxKey.BindingName,
		Configuration: json.RawMessage(bindingConfiguration),
	}}}
}

// startOutcomesRunner runs the project endpoint runner with in-memory inboxes until the test ends.
func startOutcomesRunner(t *testing.T, credentials docusign.Credentials, bindingConfiguration string, target sdkgo.TriggerTarget[docusign.EnvelopeEvent]) *docusign.EnvelopeEventReceivedEndpointRunner {
	t.Helper()
	client, err := docusign.New(docusign.Config{ConnectMaxBodyBytes: 4096}, staticCredentials(credentials))
	require.NoError(t, err)
	connection, err := docusign.NewConnection(client, testConnection)
	require.NoError(t, err)
	objects := testsupport.NewObjectStore()
	scope := projectconfig.Scope{ProjectID: "docusign-endpoint-runner-tests", Kind: "live"}
	runner, err := docusign.NewEnvelopeEventReceivedEndpointRunnerForTest(connection, outcomesConfiguration(bindingConfiguration),
		[]docusign.ProjectEnvelopeEventReceivedTriggerRoute{{BindingName: outcomesInboxKey.BindingName, Target: target}},
		func(key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[docusign.EnvelopeEvent]) (sdkgo.TriggerTarget[docusign.EnvelopeEvent], error) {
			inbox, err := projectconfig.NewTriggerInbox(objects, scope, key)
			if err != nil {
				return nil, err
			}
			return provider.NewDurableTriggerTarget(inbox, key, target)
		})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-finished, context.Canceled)
	})
	require.Eventually(t, func() bool { return runner.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	return runner
}

func deliverConnect(runner http.Handler, body string, headers map[string]string) int {
	request := httptest.NewRequest(http.MethodPost, "/docusign/connect", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	runner.ServeHTTP(response, request)
	return response.Code
}

func TestConnectEndpointRecordsAVerifiedEnvelopeOutcome(t *testing.T) {
	target := newCollectingTarget()
	runner := startOutcomesRunner(t, productionCredentials(), `{}`, target)
	body := connectBody(docusign.ConnectEventEnvelopeVoided, strings.ToUpper(testEnvelopeID), "opp-123")

	// DocuSign sends one header per account key; the connection's key may be the second one.
	require.Equal(t, http.StatusOK, deliverConnect(runner, body, map[string]string{
		"X-DocuSign-Signature-1": connectSignature("another-account-key", body), "X-DocuSign-Signature-2": connectSignature(sentinelHMACKey, body),
	}))
	event := target.next(t)
	require.Equal(t, testEnvelopeID+":envelope-voided", event.ID)
	require.Equal(t, time.Date(2026, time.October, 4, 13, 0, 0, 123000000, time.UTC), event.OccurredAt)
	require.Equal(t, docusign.EnvelopeEvent{
		Event: docusign.ConnectEventEnvelopeVoided, EnvelopeID: testEnvelopeID, AccountID: testAccountID,
		GeneratedAt: event.OccurredAt, Status: docusign.EnvelopeStatusVoided, VoidedReason: "Signer left the company",
		CustomFields: []docusign.EnvelopeCustomField{{Name: "dexSigningRequestId", Value: "opp-123"}},
	}, event.Payload)
	requestID, isFound := event.Payload.CustomFieldValue("dexSigningRequestId")
	require.True(t, isFound)
	require.Equal(t, "opp-123", requestID)
}

func TestConnectEndpointRejectsForgeriesAndAcknowledgesOtherEvents(t *testing.T) {
	target := newCollectingTarget()
	runner := startOutcomesRunner(t, productionCredentials(), `{"events":["envelope-completed"]}`, target)
	completed := connectBody(docusign.ConnectEventEnvelopeCompleted, testEnvelopeID, "opp-123")

	require.Equal(t, http.StatusBadRequest, deliverConnect(runner, completed, nil), "an unsigned delivery is refused")
	require.Equal(t, http.StatusBadRequest, deliverConnect(runner, completed, map[string]string{"X-DocuSign-Signature-1": connectSignature("not-the-key", completed)}))
	require.Equal(t, http.StatusBadRequest, deliverConnect(runner, strings.Replace(completed, "opp-123", "opp-999", 1),
		map[string]string{"X-DocuSign-Signature-1": connectSignature(sentinelHMACKey, completed)}), "a tampered body is refused")
	require.Equal(t, http.StatusBadRequest, deliverConnect(runner, completed, map[string]string{"X-DocuSign-Signature": connectSignature(sentinelHMACKey, completed)}),
		"only numbered signature headers count")

	recipientEvent := strings.Replace(completed, `"event":"envelope-completed"`, `"event":"recipient-completed"`, 1)
	require.Equal(t, http.StatusOK, deliverConnect(runner, recipientEvent, map[string]string{"X-DocuSign-Signature-1": connectSignature(sentinelHMACKey, recipientEvent)}),
		"an event the Trigger does not handle is acknowledged")
	declined := connectBody(docusign.ConnectEventEnvelopeDeclined, testEnvelopeID, "opp-123")
	require.Equal(t, http.StatusOK, deliverConnect(runner, declined, map[string]string{"X-DocuSign-Signature-1": connectSignature(sentinelHMACKey, declined)}),
		"the binding filters a declined event but still acknowledges it")
	xmlBody := `<DocuSignEnvelopeInformation/>`
	require.Equal(t, http.StatusBadRequest, deliverConnect(runner, xmlBody, map[string]string{"X-DocuSign-Signature-1": connectSignature(sentinelHMACKey, xmlBody)}),
		"a legacy XML configuration is refused")
	require.Equal(t, http.StatusRequestEntityTooLarge, deliverConnect(runner, strings.Repeat(" ", 5000)+completed, nil))

	require.Equal(t, http.StatusOK, deliverConnect(runner, completed, map[string]string{"X-DocuSign-Signature-1": connectSignature(sentinelHMACKey, completed)}))
	require.Equal(t, testEnvelopeID+":envelope-completed", target.next(t).ID)
	select {
	case event := <-target.events:
		t.Fatalf("unexpected event %s", event.ID)
	default:
	}
}

func TestConnectEndpointAnswersRetryableWithoutAnHMACKey(t *testing.T) {
	credentials := productionCredentials()
	credentials.ConnectHMACKey = sdkgo.NewSecretString("")
	runner := startOutcomesRunner(t, credentials, `{}`, newCollectingTarget())
	body := connectBody(docusign.ConnectEventEnvelopeCompleted, testEnvelopeID, "opp-123")
	require.Equal(t, http.StatusServiceUnavailable, deliverConnect(runner, body, map[string]string{"X-DocuSign-Signature-1": connectSignature("", body)}))
}

func TestConnectEndpointAcceptsAnEventWithoutEnvelopeData(t *testing.T) {
	target := newCollectingTarget()
	runner := startOutcomesRunner(t, productionCredentials(), `{}`, target)
	body := `{"event":"envelope-completed","generatedDateTime":"","data":{"accountId":"` + testAccountID + `","envelopeId":"` + testEnvelopeID + `","envelopeSummary":""}}`
	require.Equal(t, http.StatusOK, deliverConnect(runner, body, map[string]string{"X-DocuSign-Signature-1": connectSignature(sentinelHMACKey, body)}))
	event := target.next(t)
	require.Empty(t, event.Payload.CustomFields)
	require.Empty(t, event.Payload.Status)
	require.False(t, event.OccurredAt.IsZero(), "a blank generation time falls back to the receipt time")
}

func TestProjectEndpointRunnerRequiresAValidBindingInTheProjectConfiguration(t *testing.T) {
	client, err := docusign.New(docusign.Config{}, staticCredentials(productionCredentials()))
	require.NoError(t, err)
	connection, err := docusign.NewConnection(client, testConnection)
	require.NoError(t, err)
	makeDurable := func(_ projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[docusign.EnvelopeEvent]) (sdkgo.TriggerTarget[docusign.EnvelopeEvent], error) {
		return target, nil
	}
	routes := []docusign.ProjectEnvelopeEventReceivedTriggerRoute{{BindingName: "outcomes", Target: newCollectingTarget()}}

	_, err = docusign.NewEnvelopeEventReceivedEndpointRunnerForTest(connection, projectconfig.Configuration{}, routes, makeDurable)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	_, err = docusign.NewEnvelopeEventReceivedEndpointRunnerForTest(connection, outcomesConfiguration(`{"events":["envelope-sent"]}`), routes, makeDurable)
	require.ErrorContains(t, err, `binding "outcomes"`)
	_, err = docusign.NewEnvelopeEventReceivedEndpointRunnerForTest(connection, outcomesConfiguration(`{}`), append(routes, routes[0]), makeDurable)
	require.ErrorContains(t, err, "duplicated")
	inboxFailure := errors.New("project trigger inbox is unavailable")
	_, err = docusign.NewEnvelopeEventReceivedEndpointRunnerForTest(connection, outcomesConfiguration(`{}`), routes,
		func(projectconfig.TriggerInboxKey, sdkgo.TriggerTarget[docusign.EnvelopeEvent]) (sdkgo.TriggerTarget[docusign.EnvelopeEvent], error) {
			return nil, inboxFailure
		})
	require.ErrorIs(t, err, inboxFailure)
}
