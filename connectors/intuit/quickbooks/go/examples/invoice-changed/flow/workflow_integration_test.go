//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoicechanged

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go/internal/quickbooksinvoicefake"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var integrationScope = projectconfig.Scope{ProjectID: "quickbooks-invoice-changed", Kind: "live"}

// TestInvoiceChangesStartOneFlowEachAcrossARestartWithRealDex: existing invoices are a baseline, each later
// version starts exactly one Flow, and changes made while the process is stopped arrive after a restart
// whose first poll QuickBooks throttles.
func TestInvoiceChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newInvoiceChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	existing := environment.provider.Create("ORDER-1041", "58", "40.00")
	first := environment.startTriggerProcess(t)
	environment.waitForBaseline(t)

	created := environment.provider.Create("ORDER-1042", "58", "112.40")
	environment.provider.Pay(existing)
	first.runner.WakePoller(InvoiceChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, quickbooks.InvoiceChangeCreated, outcomes[created].ChangeKind)
	require.False(t, outcomes[created].IsPaid)
	require.Equal(t, quickbooks.InvoiceChangeUpdated, outcomes[existing].ChangeKind)
	require.True(t, outcomes[existing].IsPaid, "the Flow reads the paid invoice back from QuickBooks")
	first.stop(t)

	offline := environment.provider.Create("ORDER-1043", "61", "9.95")
	environment.provider.Pay(created)
	environment.provider.Throttle(1, "1")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(InvoiceChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, quickbooks.InvoiceChangeCreated, outcomes[offline].ChangeKind)
	require.True(t, outcomes[created].IsPaid)
	require.Equal(t, 4, environment.deliveryCount(), "no version is delivered twice and the baseline is never delivered")
	require.Contains(t, environment.provider.Queries()[1], "where MetaData.LastUpdatedTime >= ")
}

// TestRejectedWatermarkRecordsANewBaselineWithRealDex: a validation Fault for the watermark query reseeds
// without delivering the gap.
func TestRejectedWatermarkRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newInvoiceChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Create("ORDER-2001", "58", "10.00")
	first := environment.startTriggerProcess(t)
	environment.waitForBaseline(t)
	first.stop(t)

	gap := environment.provider.Create("ORDER-2002", "58", "20.00")
	environment.provider.RejectWatermarkQueries(true)
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.provider.RejectWatermarkQueries(false)
	afterReset := environment.provider.Create("ORDER-2003", "58", "30.00")
	second.runner.WakePoller(InvoiceChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Contains(t, outcomes, afterReset)
	require.NotContains(t, outcomes, gap, "changes between the rejected watermark and the new baseline are not delivered")
}

type invoiceChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *quickbooksinvoicefake.Invoices
	connection quickbooks.Connection
	objects    *testsupport.ObjectStore
	mutex      sync.Mutex
	delivered  map[string]sdkgo.TriggerEvent[quickbooks.InvoiceChangedEvent]
	order      []string
}

// newInvoiceChangedEnvironment uses a unique company, so Flow IDs stay unique across runs against one Dex.
func newInvoiceChangedEnvironment(t *testing.T) *invoiceChangedEnvironment {
	t.Helper()
	realmID := strconv.FormatInt(time.Now().UnixNano(), 10)
	fake := quickbooksinvoicefake.New(t, realmID)
	reference := sdkgo.ConnectionRef{Provider: "quickbooks", Name: ConnectionName}
	client, err := quickbooks.New(quickbooks.Config{PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[quickbooks.Credentials]{
		reference: {
			ClientID: "client", ClientSecret: sdkgo.NewSecretString("client-secret"),
			AccessToken: sdkgo.NewSecretString("intuit-access-token"), IDToken: sdkgo.NewSecretString(integrationIDToken(t, realmID)),
		},
	}, quickbooks.WithLocalProviderURL(fake.URL))
	require.NoError(t, err)
	connection, err := quickbooks.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &invoiceChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[quickbooks.InvoiceChangedEvent]{},
	}
}

// integrationIDToken is an unsigned Intuit-shaped ID token naming realmID.
func integrationIDToken(t *testing.T, realmID string) string {
	t.Helper()
	claims, err := json.Marshal(map[string]any{"iss": "https://oauth.platform.intuit.com/op/v1", "aud": []string{"client"}, "realmid": realmID})
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims) + ".c2ln"
}

func (environment *invoiceChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: quickbooks.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: quickbooks.InvoiceChangedTriggerDefinition.Trigger.TriggerName, BindingName: InvoiceChangedTriggerBinding,
	}
}
