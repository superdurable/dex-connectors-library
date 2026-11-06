// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := excel.New(excel.Config{}, sdkgo.StaticCredentialProvider[excel.Credentials]{
		excelConnection: {AuthMethodID: excel.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(excelTestToken)},
	}, excel.WithHTTPClient(transport), excel.WithLocalProviderURL(endpoint), excel.WithClock(conformanceFixedClock(t, scenario)))
	require.NoError(t, err)
	context := newDexContext("conformance")
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.previousHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario,
		before: func(attempt int) {
			if attempt > 1 {
				context.previousHeartbeat = append([]byte(nil), context.recordedHeartbeat...)
			}
			context.attempt = prior + int32(attempt)
		},
		actions: map[string]conformanceOperationInvocation{
			"getTableRows":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetTableRows(), excelConnection),
			"getValues":       newConformanceQueryInvocation(t, scenario.Input, context, client.GetValues(), excelConnection),
			"updateValues":    newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateValues(), excelConnection, scenario.CallID),
			"appendTableRows": newConformanceMutationInvocation(t, scenario.Input, context, client.AppendTableRows(), excelConnection, scenario.CallID),
		},
	}
}
