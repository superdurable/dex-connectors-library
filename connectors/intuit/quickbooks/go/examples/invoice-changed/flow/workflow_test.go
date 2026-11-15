// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoicechanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind quickbooks.InvoiceChangeKind) sdkgo.TriggerEvent[quickbooks.InvoiceChangedEvent] {
	return sdkgo.TriggerEvent[quickbooks.InvoiceChangedEvent]{
		ID: "qbo-invoice-0123456789abcdef0123456789abcdef",
		Payload: quickbooks.InvoiceChangedEvent{
			RealmID: "9341453050298464", Environment: quickbooks.EnvironmentProduction, ChangeKind: kind,
			Invoice: quickbooks.Invoice{InvoiceID: "131", SyncToken: "1", CustomerID: "58", TotalAmount: "75.00", Balance: "0"},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerInvoiceVersion(t *testing.T) {
	event := sampleTriggerEvent(quickbooks.InvoiceChangeUpdated)
	require.Equal(t, "quickbooks-invoice-change-qbo-invoice-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryInvoiceChange(event))
	require.Equal(t, quickbooks.GetInvoiceInput{InvoiceID: "131"}, MapToGetInvoiceInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []quickbooks.InvoiceChangeKind{quickbooks.InvoiceChangeCreated, quickbooks.InvoiceChangeUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(quickbooks.InvoiceChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingInvoice := MapToFlowInput(sampleTriggerEvent(quickbooks.InvoiceChangeCreated))
	missingInvoice.Change.Invoice.InvoiceID = ""
	require.ErrorContains(t, validateInput(missingInvoice), "change.invoice.invoiceId")
	unknownKind := MapToFlowInput(sampleTriggerEvent("deleted"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingHasNoVisibleFields(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, quickbooks.InvoiceChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, InvoiceChangedTriggerBinding, binding.BindingName)
	require.Nil(t, binding.ConfigurationUI, "the binding watches the whole company, so Dex Web shows no Trigger field")
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(quickbooks.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordInvoiceChangeStepType, dex.GetFinalStepType[Input](recordInvoiceChange{}))
	require.Equal(t, completeInvoiceChangeStepType, dex.GetFinalStepType[quickbooks.GetInvoiceResult](completeInvoiceChange{}))
	wait, err := recordInvoiceChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := quickbooks.New(quickbooks.Config{}, sdkgo.StaticCredentialProvider[quickbooks.Credentials]{})
	require.NoError(t, err)
	connection, err := quickbooks.NewConnection(client, sdkgo.ConnectionRef{Provider: "quickbooks", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
