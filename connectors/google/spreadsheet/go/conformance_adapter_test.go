// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	spreadsheet "github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "/v4", "/v4"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
	}, spreadsheet.WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("conformance", "sheets-operation")
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) {
		require.NoError(t, context.SetPersistedCheckpoint(json.RawMessage(raw)))
	})
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		for context.Attempt() < prior+int32(attempt) {
			context.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getValues": newConformanceQueryInvocation(t, scenario.Input, context, client.GetValues(), sheetsConnection),
		"findRow":   newConformanceQueryInvocation(t, scenario.Input, context, client.FindRow(), sheetsConnection),
		"upsertRow": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertRow(), sheetsConnection, scenario.CallID),
	}}
}
