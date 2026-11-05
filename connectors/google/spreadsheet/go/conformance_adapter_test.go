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

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
	}, spreadsheet.WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("conformance-flow", "conformance")
	if len(scenario.PriorHeartbeat) > 0 {
		var marker any
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		require.NoError(t, context.SetPersistedCheckpoint(marker))
		var prior struct {
			Attempt int `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
		for range prior.Attempt {
			context.AdvanceAttempt()
		}
	}
	first := true
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(int) {
		if first {
			first = false
			return
		}
		context.AdvanceAttempt()
	}, actions: map[string]conformanceOperationInvocation{
		"getValues": newConformanceQueryInvocation(t, scenario.Input, context, client.GetValues(), sheetsConnection),
		"findRow":   newConformanceQueryInvocation(t, scenario.Input, context, client.FindRow(), sheetsConnection),
		"upsertRow": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertRow(), sheetsConnection, scenario.CallID),
	}}
}
