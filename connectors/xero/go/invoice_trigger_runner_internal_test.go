// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go/internal/xeroinvoicefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "xero-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func newTriggerTestConnection(t *testing.T, fake *xeroinvoicefake.Organisation) Connection {
	t.Helper()
	connection, err := NewConnection(newTriggerTestClient(t, fake), triggerConnection)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[InvoiceChangedEvent]) sdkgo.TriggerTarget[InvoiceChangedEvent] {
	return sdkgo.TriggerTargetFunc[InvoiceChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[InvoiceChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewInvoiceTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, xeroinvoicefake.New(t))
	target := channelTarget(make(chan sdkgo.TriggerEvent[InvoiceChangedEvent], 1))
	for name, routes := range map[string][]InvoiceChangedTriggerRoute{
		"none":         nil,
		"no binding":   {{Target: target}},
		"no target":    {{BindingName: "salesInvoices"}},
		"duplicate":    {{BindingName: "salesInvoices", Target: target}, {BindingName: "salesInvoices", Target: target}},
		"invalid type": {{BindingName: "salesInvoices", Target: target, Configuration: InvoiceChangedTriggerConfiguration{Type: "SALES"}}},
	} {
		_, err := NewInvoiceTriggerRunner(InvoiceTriggerRunnerConfig{Connection: connection, InvoiceChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewInvoiceTriggerRunner(InvoiceTriggerRunnerConfig{InvoiceChangedRoutes: []InvoiceChangedTriggerRoute{{BindingName: "salesInvoices", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
	_, err = New(Config{PollInterval: 10 * time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "at least 30s")
}

func TestDurableInvoiceTriggerRunnerLoadsTheSavedFilterAndCommitsAfterRecording(t *testing.T) {
	fake := xeroinvoicefake.New(t)
	fake.Upsert(triggerInvoiceA, "INV-0001", "ACCREC", "AUTHORISED", "49.95")
	fake.AdvanceClock(time.Minute)
	connection := newTriggerTestConnection(t, fake)
	saved, err := json.Marshal(InvoiceChangedTriggerConfiguration{Statuses: []InvoiceStatus{InvoiceStatusPaid}})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: triggerConnection.Name, TriggerName: "invoiceChanged", BindingName: "paidInvoices", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[InvoiceChangedEvent], 4)
	runner, err := newDurableInvoiceTriggerRunner(connection, configuration, storage, ProjectInvoiceTriggerRunnerConfig{
		InvoiceChangedRoutes: []ProjectInvoiceChangedTriggerRoute{{BindingName: "paidInvoices", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: triggerConnection.Name, TriggerName: "invoiceChanged", BindingName: "paidInvoices"}
	cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-result, context.Canceled) })

	committedSince := func() int64 {
		record, err := cursor.ReadPollCursor(context.Background())
		if err != nil || record.State == nil {
			return 0
		}
		var position invoiceChangeCursor
		require.NoError(t, json.Unmarshal([]byte(record.State.Token), &position))
		require.Equal(t, "type=;statuses=PAID", position.Filter)
		return position.Since
	}
	require.Eventually(t, func() bool { return committedSince() != 0 }, 10*time.Second, 10*time.Millisecond)
	fake.Upsert(triggerInvoiceB, "INV-0002", "ACCREC", "AUTHORISED", "20.00")
	paidAt := fake.SetStatus(triggerInvoiceA, "PAID")
	runner.WakePoller("paidInvoices")

	select {
	case event := <-events:
		require.Equal(t, triggerInvoiceA, event.Payload.Invoice.InvoiceID, "only the saved PAID filter matches")
		require.Equal(t, InvoiceStatusPaid, event.Payload.Invoice.Status)
	case <-time.After(10 * time.Second):
		t.Fatal("the paid invoice was not delivered")
	}
	require.Eventually(t, func() bool { return committedSince() == paidAt.UnixMilli() }, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, "PAID", fake.Requests()[0].Query.Get("Statuses"))
}

func TestGeneratedInvoiceChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	fake := xeroinvoicefake.New(t)
	fake.Upsert(triggerInvoiceA, "INV-0001", "ACCREC", "DRAFT", "49.95")
	connection := newTriggerTestConnection(t, fake)
	events := make(chan sdkgo.TriggerEvent[InvoiceChangedEvent], 4)
	trigger := NewInvoiceChangedTrigger(InvoiceChangedTriggerConfig{
		Connection: connection, ConnectionName: triggerConnection.Name, BindingName: "allInvoices", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(fake.Requests()) > 0 }, 10*time.Second, 10*time.Millisecond)
	fake.SetStatus(triggerInvoiceA, "AUTHORISED")

	select {
	case event := <-events:
		require.Equal(t, InvoiceStatusAuthorised, event.Payload.Invoice.Status)
		require.Empty(t, event.Payload.TenantID, "a Custom Connection sends no tenant")
	case <-time.After(45 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}
