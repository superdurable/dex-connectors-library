// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoicechanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sampleInvoiceID = "7b3db23e-1c22-43a1-b11e-dac2e5637d70"

func sampleTriggerEvent(status xero.InvoiceStatus) sdkgo.TriggerEvent[xero.InvoiceChangedEvent] {
	return sdkgo.TriggerEvent[xero.InvoiceChangedEvent]{
		ID: "xero-invoice-0123456789abcdef0123456789abcdef",
		Payload: xero.InvoiceChangedEvent{Invoice: xero.Invoice{
			InvoiceID: sampleInvoiceID, InvoiceNumber: "INV-1042", Type: xero.InvoiceTypeAccountsReceivable, Status: status,
		}},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent(xero.InvoiceStatusPaid)
	require.Equal(t, "xero-invoice-change-xero-invoice-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryInvoiceChange(event))
	require.Equal(t, xero.GetInvoiceInput{InvoiceID: sampleInvoiceID}, MapToGetInvoiceInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(xero.InvoiceStatusDeleted))))
	missingID := MapToFlowInput(sampleTriggerEvent(xero.InvoiceStatusPaid))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingInvoice := MapToFlowInput(sampleTriggerEvent(xero.InvoiceStatusPaid))
	missingInvoice.Change.Invoice.InvoiceID = ""
	require.ErrorContains(t, validateInput(missingInvoice), "change.invoice.invoiceId")
	missingStatus := MapToFlowInput(sampleTriggerEvent(""))
	require.ErrorContains(t, validateInput(missingStatus), "change.invoice.status")
}

func TestTriggerBindingShowsOneGuidedInvoiceFilter(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, xero.InvoiceChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, InvoiceChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the filter is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, xero.UIUnitInvoiceChangeFilter, unit.UnitID)
	require.Equal(t, "Invoices that start this Flow", unit.Label)
	require.False(t, unit.Required, "an unsaved filter receives every invoice and bill")
	for _, guidance := range []string{"ACCREC", "ACCPAY", "PAID", "unsaved", "every invoice and bill", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: xero.UIInvoiceChangeFilterPortType, JSONPointer: "/type"},
		{Port: xero.UIInvoiceChangeFilterPortStatuses, JSONPointer: "/statuses"},
	}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(xero.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordInvoiceChangeStepType, dex.GetFinalStepType[Input](recordInvoiceChange{}))
	require.Equal(t, completeInvoiceChangeStepType, dex.GetFinalStepType[xero.GetInvoiceResult](completeInvoiceChange{}))
	wait, err := recordInvoiceChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := xero.New(xero.Config{}, sdkgo.StaticCredentialProvider[xero.Credentials]{})
	require.NoError(t, err)
	connection, err := xero.NewConnection(client, sdkgo.ConnectionRef{Provider: "xero", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
