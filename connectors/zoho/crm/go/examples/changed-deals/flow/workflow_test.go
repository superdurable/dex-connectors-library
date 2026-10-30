// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package changeddeals

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName))
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordChangedDealsRequestStepType, dex.GetFinalStepType[Input](recordChangedDealsRequest{}))
	require.Equal(t, collectChangedZohoDealsStepType, dex.GetFinalStepType[crm.ListModifiedRecordsResult](collectChangedZohoDeals{}))
	_, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"))})
	})
}

func TestBuildFirstPageAcceptsExactlyOneStartingPoint(t *testing.T) {
	page, maxPages, err := BuildFirstPage(Input{ModifiedSince: "2026-01-28T18:30:00+05:30"})
	require.NoError(t, err)
	require.True(t, page.ModifiedSince.Equal(time.Date(2026, 1, 28, 13, 0, 0, 0, time.UTC)))
	require.Equal(t, DefaultMaxPages, maxPages)
	page, maxPages, err = BuildFirstPage(Input{Cursor: " 2026-01-28T13:00:05Z/4150868000003194012 ", MaxPages: 2})
	require.NoError(t, err)
	require.Equal(t, ChangedDealsPage{Cursor: "2026-01-28T13:00:05Z/4150868000003194012"}, page)
	require.Equal(t, 2, maxPages)
	for name, input := range map[string]Input{
		"neither":         {},
		"both":            {ModifiedSince: "2026-01-28T13:00:00Z", Cursor: "2026-01-28T13:00:05Z/0"},
		"date only":       {ModifiedSince: "2026-01-28"},
		"foreign cursor":  {Cursor: "c8582xx9e7c7"},
		"too many pages":  {ModifiedSince: "2026-01-28T13:00:00Z", MaxPages: 21},
		"negative budget": {ModifiedSince: "2026-01-28T13:00:00Z", MaxPages: -1},
	} {
		_, _, err := BuildFirstPage(input)
		require.Error(t, err, name)
	}
}

func TestChangedDealsFromRecordsReadsTheSelectedFields(t *testing.T) {
	deals := ChangedDealsFromRecords([]crm.Record{{Module: crm.ModuleDeals, ID: "7", Fields: map[string]json.RawMessage{
		crm.FieldDealName: crm.TextFieldValue("Acme expansion"), crm.FieldStage: crm.TextFieldValue("Closed Won"),
		crm.FieldModifiedTime: crm.TextFieldValue("2026-01-28T18:30:05+05:30"),
	}}})
	require.Len(t, deals, 1)
	require.Equal(t, "Acme expansion", deals[0].DealName)
	require.Equal(t, "Closed Won", deals[0].Stage)
	require.True(t, deals[0].ModifiedAt.Equal(time.Date(2026, 1, 28, 13, 0, 5, 0, time.UTC)))
}

func newUnitTestConnection(t *testing.T, name string) crm.Connection {
	t.Helper()
	client, err := crm.New(crm.Config{}, sdkgo.StaticCredentialProvider[crm.Credentials]{})
	require.NoError(t, err)
	connection, err := crm.NewConnection(client, sdkgo.ConnectionRef{Provider: "zoho", Name: name})
	require.NoError(t, err)
	return connection
}
